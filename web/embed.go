// Package web holds io7's frontend, embedded into the binary. There is no
// build step: plain HTML/JS/CSS plus a vendored CodeMirror 6 bundle under
// static/vendor/ (committed, fetched once — see IOX_PLAN.md "Frontend").
package web

import (
	"embed"
	"io/fs"
)

//go:embed static
var static embed.FS

// Assets is the frontend file tree rooted at the static directory.
var Assets fs.FS

func init() {
	var err error
	Assets, err = fs.Sub(static, "static")
	if err != nil {
		panic(err)
	}
}
