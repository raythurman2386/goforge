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

// jsDir holds the scripts Base loads, relative to the working directory.
const jsDir = "static/js/"

// baseScripts are the scripts Base loads, in order. Their URLs carry a content
// hash like the stylesheet: /static responses have no Cache-Control, so a
// browser or the CDN keeps a script for hours, and a page from a new deploy
// would run with an older script (for example a ui.js without the Sheet
// runtime, so the mobile menu button does nothing).
var baseScripts = []string{"htmx.min.js", "hx-sse.min.js", "app.js", "ui.js"}

// htmxConfig is the <meta name="htmx-config"> Base renders before htmx loads.
// htmx 4 swaps every response except 204 and 304; GoForge keeps the htmx 2
// rule that 4xx and 5xx responses do not swap, so an error fragment never
// replaces a polled target. app.js turns the error text into a toast and
// ui.js shows it in the dialog's error node.
const htmxConfig = `{"noSwap":[204,304,"4xx","5xx"]}`

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

// jsVersions maps each of baseScripts to a short content hash, computed once
// per process.
var jsVersions = sync.OnceValue(func() map[string]string {
	m := make(map[string]string, len(baseScripts))
	for _, name := range baseScripts {
		m[name] = fileVersion(jsDir + name)
	}
	return m
})

// jsHref is the cache-busted URL of a script in static/js used by Base.
func jsHref(name string) string {
	v, ok := jsVersions()[name]
	if !ok {
		v = fileVersion(jsDir + name)
	}
	return "/static/js/" + name + "?v=" + v
}
