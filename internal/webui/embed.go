package webui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed ui
var uiFS embed.FS

// Handler serves the embedded static web UI. Assets are compiled into the
// binary, so redeploying requires a fresh build; no-cache headers make sure a
// browser does not keep serving a stale copy of the shell or scripts.
func Handler() http.Handler {
	sub, err := fs.Sub(uiFS, "ui")
	if err != nil {
		panic(err)
	}
	files := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		files.ServeHTTP(w, r)
	})
}
