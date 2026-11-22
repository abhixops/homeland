package api

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log"
	"os"
	"path/filepath"
)

// cachedAssets are the stylesheet and script files the page links to. Their
// combined content hash is added to their URLs (?v=...), so a new build always
// gets new URLs and browsers or CDNs (e.g. Cloudflare) can't keep serving a
// stale copy next to the new HTML.
var cachedAssets = []string{
	filepath.Join("css", "tailwind.css"),
	filepath.Join("js", "app.js"),
}

// AssetVersion returns a short hash of the linked static assets under staticDir.
// It returns "dev" if none of them can be read.
func AssetVersion(staticDir string) string {
	h := sha256.New()
	read := 0
	for _, name := range cachedAssets {
		f, err := os.Open(filepath.Join(staticDir, name))
		if err != nil {
			log.Printf("[api] asset version: %v", err)
			continue
		}
		_, err = io.Copy(h, f)
		f.Close()
		if err != nil {
			log.Printf("[api] asset version: read %s: %v", name, err)
			continue
		}
		read++
	}
	if read == 0 {
		return "dev"
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}
