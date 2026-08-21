package webassets

import (
	"embed"
	"io/fs"
)

//go:embed static/*
var staticFiles embed.FS

func Static() (fs.FS, error) {
	return fs.Sub(staticFiles, "static")
}
