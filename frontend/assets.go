// Package frontend owns the browser assets compiled into the Web binary.
package frontend

import (
	"embed"
	"io/fs"
)

//go:embed templates static
var assets embed.FS

var (
	// Templates contains the production Go template tree.
	Templates = mustSub("templates")
	// Static contains CSS and JavaScript served under /static.
	Static = mustSub("static")
)

func mustSub(directory string) fs.FS {
	result, err := fs.Sub(assets, directory)
	if err != nil {
		panic(err)
	}
	return result
}
