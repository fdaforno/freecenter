package web

import (
	"bytes"
	"compress/gzip"
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"regexp"
	"strings"
	"sync"
)

// dist is the Angular build output of ./ui (see `make ui`).
//
//go:embed all:dist
var assets embed.FS

// FS returns the embedded UI files.
func FS() fs.FS {
	sub, err := fs.Sub(assets, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}

// hashedAsset matches build outputs whose name carries a content hash
// (main-HGQIRWEG.js): they can be cached forever.
var hashedAsset = regexp.MustCompile(`-[A-Z0-9]{8}\.(js|css)$`)

// Handler serves the embedded UI. Embedded files have no modification time,
// so without explicit headers browsers would re-download the whole bundle on
// every load; text assets are also gzip-compressed (once, then cached).
func Handler() http.Handler {
	files := FS()
	fileServer := http.FileServerFS(files)
	var compressed sync.Map // file name -> []byte

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		if hashedAsset.MatchString(name) {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}

		ext := path.Ext(name)
		compressible := ext == ".js" || ext == ".css" || ext == ".html"
		if !compressible || !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			fileServer.ServeHTTP(w, r)
			return
		}
		body, ok := compressed.Load(name)
		if !ok {
			raw, err := fs.ReadFile(files, name)
			if err != nil {
				fileServer.ServeHTTP(w, r) // let the file server produce the 404
				return
			}
			var buf bytes.Buffer
			zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
			_, _ = zw.Write(raw)
			_ = zw.Close()
			body, _ = compressed.LoadOrStore(name, buf.Bytes())
		}
		w.Header().Set("Content-Type", mime.TypeByExtension(ext))
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Vary", "Accept-Encoding")
		_, _ = w.Write(body.([]byte))
	})
}
