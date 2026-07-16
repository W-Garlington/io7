// Package graphdb is a minimal cgo binding against LadybugDB's C API
// (lbug.h). It deliberately avoids the official go-ladybug module and its
// Apache Arrow dependency tree — see IOX_PLAN.md "Graph DB binding".
//
// The surface is intentionally small: open a database, run one Cypher
// statement (optionally with $param bindings), get fully-materialized rows
// back. No C memory outlives a call.
package graphdb

/*
#cgo CFLAGS: -I${SRCDIR}/lib-ladybug
#cgo linux LDFLAGS: -L${SRCDIR}/lib-ladybug -llbug -Wl,-rpath,${SRCDIR}/lib-ladybug
#cgo darwin LDFLAGS: -L${SRCDIR}/lib-ladybug -llbug -Wl,-rpath,${SRCDIR}/lib-ladybug
#cgo windows LDFLAGS: -L${SRCDIR}/lib-ladybug -llbug
#include <stdlib.h>
#include "lbug.h"
*/
import "C"

import (
	"fmt"
	"sync"
	"time"
	"unsafe"
)

//go:generate sh -c "curl -fsSL https://raw.githubusercontent.com/LadybugDB/ladybug/refs/heads/main/scripts/download-liblbug.sh | LBUG_TARGET_DIR=$PWD/lib-ladybug bash"

// Row is one result tuple, keyed by column name.
type Row map[string]any

// DB is an open LadybugDB database with a single connection. All calls are
// serialized by an internal mutex — fine for a single-user local process.
type DB struct {
	mu     sync.Mutex
	db     C.lbug_database
	conn   C.lbug_connection
	closed bool
}

// Open creates or opens the database at path.
func Open(path string) (*DB, error) {
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))

	d := &DB{}
	if C.lbug_database_init(cPath, C.lbug_default_system_config(), &d.db) != C.LbugSuccess {
		return nil, fmt.Errorf("graphdb: open database %q failed", path)
	}
	if C.lbug_connection_init(&d.db, &d.conn) != C.LbugSuccess {
		C.lbug_database_destroy(&d.db)
		return nil, fmt.Errorf("graphdb: connect to database %q failed", path)
	}
	return d, nil
}

// Close releases the connection and database. Safe to call more than once.
func (d *DB) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}
	d.closed = true
	C.lbug_connection_destroy(&d.conn)
	C.lbug_database_destroy(&d.db)
}

// Exec runs a single Cypher statement and discards any rows.
func (d *DB) Exec(cypher string, params map[string]any) error {
	_, err := d.Query(cypher, params)
	return err
}

// Query runs a single Cypher statement and returns all result rows.
// Values in params are bound to $name placeholders; supported types are
// string, bool, int, int64, float64, and time.Time.
func (d *DB) Query(cypher string, params map[string]any) ([]Row, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil, fmt.Errorf("graphdb: database is closed")
	}

	var result C.lbug_query_result
	if len(params) == 0 {
		cQuery := C.CString(cypher)
		defer C.free(unsafe.Pointer(cQuery))
		C.lbug_connection_query(&d.conn, cQuery, &result)
	} else {
		stmt, err := d.prepare(cypher, params)
		if err != nil {
			return nil, err
		}
		defer C.lbug_prepared_statement_destroy(stmt)
		C.lbug_connection_execute(&d.conn, stmt, &result)
	}
	defer C.lbug_query_result_destroy(&result)

	if !C.lbug_query_result_is_success(&result) {
		return nil, resultError(&result)
	}
	return collectRows(&result)
}

// prepare compiles cypher and binds params. The caller destroys the statement.
func (d *DB) prepare(cypher string, params map[string]any) (*C.lbug_prepared_statement, error) {
	cQuery := C.CString(cypher)
	defer C.free(unsafe.Pointer(cQuery))

	stmt := &C.lbug_prepared_statement{}
	C.lbug_connection_prepare(&d.conn, cQuery, stmt)
	if !C.lbug_prepared_statement_is_success(stmt) {
		msg := C.lbug_prepared_statement_get_error_message(stmt)
		defer C.lbug_destroy_string(msg)
		C.lbug_prepared_statement_destroy(stmt)
		return nil, fmt.Errorf("graphdb: prepare failed: %s", C.GoString(msg))
	}

	for name, value := range params {
		if err := bindParam(stmt, name, value); err != nil {
			C.lbug_prepared_statement_destroy(stmt)
			return nil, err
		}
	}
	return stmt, nil
}

func bindParam(stmt *C.lbug_prepared_statement, name string, value any) error {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))

	var state C.lbug_state
	switch v := value.(type) {
	case string:
		cVal := C.CString(v)
		defer C.free(unsafe.Pointer(cVal))
		state = C.lbug_prepared_statement_bind_string(stmt, cName, cVal)
	case bool:
		state = C.lbug_prepared_statement_bind_bool(stmt, cName, C.bool(v))
	case int:
		state = C.lbug_prepared_statement_bind_int64(stmt, cName, C.int64_t(v))
	case int64:
		state = C.lbug_prepared_statement_bind_int64(stmt, cName, C.int64_t(v))
	case float64:
		state = C.lbug_prepared_statement_bind_double(stmt, cName, C.double(v))
	case time.Time:
		ts := C.lbug_timestamp_t{value: C.int64_t(v.UnixMicro())}
		state = C.lbug_prepared_statement_bind_timestamp(stmt, cName, ts)
	default:
		return fmt.Errorf("graphdb: unsupported param type %T for $%s", value, name)
	}
	if state != C.LbugSuccess {
		return fmt.Errorf("graphdb: binding $%s failed", name)
	}
	return nil
}

func collectRows(result *C.lbug_query_result) ([]Row, error) {
	numCols := uint64(C.lbug_query_result_get_num_columns(result))
	columns := make([]string, numCols)
	for i := range columns {
		var cName *C.char
		if C.lbug_query_result_get_column_name(result, C.uint64_t(i), &cName) != C.LbugSuccess {
			return nil, fmt.Errorf("graphdb: reading column name %d failed", i)
		}
		columns[i] = C.GoString(cName)
		C.lbug_destroy_string(cName)
	}

	var rows []Row
	for C.lbug_query_result_has_next(result) {
		var tuple C.lbug_flat_tuple
		if C.lbug_query_result_get_next(result, &tuple) != C.LbugSuccess {
			return nil, fmt.Errorf("graphdb: reading next tuple failed")
		}
		row := make(Row, numCols)
		for i, col := range columns {
			var value C.lbug_value
			if C.lbug_flat_tuple_get_value(&tuple, C.uint64_t(i), &value) != C.LbugSuccess {
				C.lbug_flat_tuple_destroy(&tuple)
				return nil, fmt.Errorf("graphdb: reading column %q failed", col)
			}
			row[col] = goValue(&value)
			C.lbug_value_destroy(&value)
		}
		C.lbug_flat_tuple_destroy(&tuple)
		rows = append(rows, row)
	}
	return rows, nil
}

// goValue converts a lbug_value into a plain Go value. Types outside the
// schema's needs fall back to their string rendering.
func goValue(value *C.lbug_value) any {
	if C.lbug_value_is_null(value) {
		return nil
	}
	var lt C.lbug_logical_type
	C.lbug_value_get_data_type(value, &lt)
	id := C.lbug_data_type_get_id(&lt)
	C.lbug_data_type_destroy(&lt)

	switch id {
	case C.LBUG_BOOL:
		var v C.bool
		C.lbug_value_get_bool(value, &v)
		return bool(v)
	case C.LBUG_INT64, C.LBUG_SERIAL:
		var v C.int64_t
		C.lbug_value_get_int64(value, &v)
		return int64(v)
	case C.LBUG_INT32:
		var v C.int32_t
		C.lbug_value_get_int32(value, &v)
		return int64(v)
	case C.LBUG_DOUBLE:
		var v C.double
		C.lbug_value_get_double(value, &v)
		return float64(v)
	case C.LBUG_FLOAT:
		var v C.float
		C.lbug_value_get_float(value, &v)
		return float64(v)
	case C.LBUG_STRING, C.LBUG_UUID:
		var v *C.char
		C.lbug_value_get_string(value, &v)
		defer C.lbug_destroy_string(v)
		return C.GoString(v)
	case C.LBUG_TIMESTAMP:
		var v C.lbug_timestamp_t
		C.lbug_value_get_timestamp(value, &v)
		return time.UnixMicro(int64(v.value)).UTC()
	default:
		cStr := C.lbug_value_to_string(value)
		defer C.lbug_destroy_string(cStr)
		return C.GoString(cStr)
	}
}

func resultError(result *C.lbug_query_result) error {
	msg := C.lbug_query_result_get_error_message(result)
	defer C.lbug_destroy_string(msg)
	return fmt.Errorf("graphdb: query failed: %s", C.GoString(msg))
}
