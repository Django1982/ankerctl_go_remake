package web

import (
	"net/http"
	"strings"
)

// noCacheAppAssets wraps a static file handler so that our own JS and CSS
// files (ankersrv.*) are served with Cache-Control: no-store.  Vendor assets
// under /static/vendor/ are unaffected and can be cached by the browser.
//
// Background: Go's embed.FS always returns ModTime=zero, so http.FileServer
// produces ETags based on file size only.  A rebuild that changes JS content
// without changing its byte length would cause browsers to receive a 304 and
// serve stale code.  no-store prevents this entirely for our mutable assets.
func noCacheAppAssets(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if isAppAsset(path) {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func isAppAsset(path string) bool {
	if strings.HasPrefix(path, "/static/vendor/") {
		return false
	}
	return strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".css")
}
