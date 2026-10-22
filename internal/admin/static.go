package admin

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

// The web UI is plain ES modules, embedded so the admin binary is one file. It
// builds its DOM with textContent only (no innerHTML), and the CSP below forbids
// inline script, eval and any outside origin, so a hostile role name or mail text
// cannot become script in an operator's browser.
//
//go:embed web
var webFiles embed.FS

const contentSecurityPolicy = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; " +
	"connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

var contentTypes = map[string]string{
	".html": "text/html; charset=utf-8",
	".js":   "text/javascript; charset=utf-8",
	".css":  "text/css; charset=utf-8",
	".svg":  "image/svg+xml",
	".json": "application/json; charset=utf-8",
}

// staticHandler serves the embedded UI. It serves files only: a directory, an
// unknown path or anything under /api/ is a 404, never a listing or a fallback page.
func staticHandler() http.Handler {
	root, _ := fs.Sub(webFiles, "web")
	etags := map[string]string{}
	fs.WalkDir(root, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			b, _ := fs.ReadFile(root, p)
			sum := sha256.Sum256(b)
			etags[p] = `"` + hex.EncodeToString(sum[:8]) + `"`
		}
		return nil
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		p := path.Clean("/" + r.URL.Path)
		if p == "/api" || strings.HasPrefix(p, "/api/") {
			fail(w, http.StatusNotFound, "no such endpoint")
			return
		}
		if p == "/" {
			p = "/index.html"
		}
		name := strings.TrimPrefix(p, "/")
		etag, ok := etags[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		f, err := root.Open(name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		rs, ok := f.(interface {
			Read([]byte) (int, error)
			Seek(int64, int) (int64, error)
		})
		if !ok {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		h := w.Header()
		if ct, ok := contentTypes[path.Ext(name)]; ok {
			h.Set("Content-Type", ct)
		}
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-cache") // revalidate: the ETag makes that cheap
		h.Set("ETag", etag)
		http.ServeContent(w, r, name, time.Time{}, rs)
	})
}
