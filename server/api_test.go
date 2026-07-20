package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/W-Garlington/io7/store"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(st.Close)
	ts := httptest.NewServer(New(st, func() {}).Handler())
	t.Cleanup(ts.Close)
	return ts
}

func doJSON(t *testing.T, method, url string, body any, wantStatus int, out any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encoding body: %v", err)
		}
	}
	req, err := http.NewRequest(method, url, &buf)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer res.Body.Close()
	if res.StatusCode != wantStatus {
		t.Fatalf("%s %s: status %d, want %d", method, url, res.StatusCode, wantStatus)
	}
	if out != nil {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			t.Fatalf("decoding response: %v", err)
		}
	}
}

func TestDocsAPI(t *testing.T) {
	ts := newTestServer(t)

	var created store.Document
	doJSON(t, "POST", ts.URL+"/api/docs",
		map[string]string{"title": "Note", "content": "body"},
		http.StatusCreated, &created)
	if created.ID == "" || created.Title != "Note" {
		t.Fatalf("unexpected created doc: %#v", created)
	}

	var got store.Document
	doJSON(t, "GET", ts.URL+"/api/docs/"+created.ID, nil, http.StatusOK, &got)
	if got.Content != "body" {
		t.Errorf("unexpected content: %q", got.Content)
	}

	var updated store.Document
	doJSON(t, "PUT", ts.URL+"/api/docs/"+created.ID,
		map[string]string{"title": "Note 2", "content": "body 2"},
		http.StatusOK, &updated)
	if updated.Title != "Note 2" {
		t.Errorf("unexpected updated doc: %#v", updated)
	}

	var docs []store.Document
	doJSON(t, "GET", ts.URL+"/api/docs", nil, http.StatusOK, &docs)
	if len(docs) != 1 {
		t.Errorf("got %d docs, want 1", len(docs))
	}

	doJSON(t, "DELETE", ts.URL+"/api/docs/"+created.ID, nil, http.StatusNoContent, nil)
	doJSON(t, "GET", ts.URL+"/api/docs/"+created.ID, nil, http.StatusNotFound, nil)
}

func TestReferencesAPI(t *testing.T) {
	ts := newTestServer(t)

	var target, src store.Document
	doJSON(t, "POST", ts.URL+"/api/docs",
		map[string]string{"title": "Target", "content": "the target"},
		http.StatusCreated, &target)
	doJSON(t, "POST", ts.URL+"/api/docs",
		map[string]string{"title": "Source", "content": "see [[supports::Target]]"},
		http.StatusCreated, &src)

	var refs store.DocReferences
	doJSON(t, "GET", ts.URL+"/api/docs/"+target.ID+"/references", nil, http.StatusOK, &refs)
	if len(refs.Incoming) != 1 || refs.Incoming[0].DocID != src.ID ||
		refs.Incoming[0].Type != "supports" {
		t.Errorf("unexpected incoming refs: %#v", refs.Incoming)
	}
	doJSON(t, "GET", ts.URL+"/api/docs/nope/references", nil, http.StatusNotFound, nil)

	var types []string
	doJSON(t, "GET", ts.URL+"/api/reftypes", nil, http.StatusOK, &types)
	found := false
	for _, tp := range types {
		if tp == "supports" {
			found = true
		}
	}
	if !found {
		t.Errorf("reftypes %v missing %q", types, "supports")
	}
}

func TestFrontendServed(t *testing.T) {
	ts := newTestServer(t)
	res, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /: status %d", res.StatusCode)
	}
	var body bytes.Buffer
	body.ReadFrom(res.Body)
	if !strings.Contains(body.String(), `id="workspace"`) {
		t.Error("index.html does not contain the workspace element")
	}
}

func TestWebSocketBroadcast(t *testing.T) {
	ts := newTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket.Dial: %v", err)
	}
	defer conn.CloseNow()

	var created store.Document
	doJSON(t, "POST", ts.URL+"/api/docs",
		map[string]string{"title": "live", "content": ""},
		http.StatusCreated, &created)

	_, msg, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("websocket read: %v", err)
	}
	var ev struct {
		Type string          `json:"type"`
		Doc  *store.Document `json:"doc"`
	}
	if err := json.Unmarshal(msg, &ev); err != nil {
		t.Fatalf("unmarshaling event: %v", err)
	}
	if ev.Type != "doc.created" || ev.Doc == nil || ev.Doc.ID != created.ID {
		t.Errorf("unexpected event: %s", msg)
	}
}
