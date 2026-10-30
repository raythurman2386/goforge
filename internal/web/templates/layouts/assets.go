// Package layouts holds the page shells (Base, AuthLayout, AppLayout) shared by
// every HTML page.
package layouts

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"sync"
)

// cssPath is the compiled stylesheet, relative to the working directory. The
// router serves /static from the same relative "static" directory.
const cssPath = "static/css/output.css"

// devVersion is the cache-bust value when output.css cannot be read.
const devVersion = "dev"

// cssVersion is a short content hash of output.css, computed once per process.
// A restyle changes the hash, so browsers fetch the new file instead of a
// cached copy. It falls back to "dev" when the file is missing (tests, or a
// checkout without `make css`).
var cssVersion = sync.OnceValue(func() string { return fileVersion(cssPath) })

func fileVersion(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return devVersion
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:12]
}

// cssHref is the cache-busted stylesheet URL used by Base.
func cssHref() string {
	return "/static/css/output.css?v=" + cssVersion()
}
