// Package web embeds the built single-page app and the built-in themes.
//
// Build the app first (npm run build); `make build` does both steps.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

//go:embed src/themes/*.json
var themes embed.FS

// Dist returns the built app rooted at dist/.
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}

// Themes returns the built-in theme JSON files.
func Themes() fs.FS {
	sub, err := fs.Sub(themes, "src/themes")
	if err != nil {
		panic(err)
	}
	return sub
}
