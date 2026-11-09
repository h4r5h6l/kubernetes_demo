// Package web serves the server-rendered HTML interface.
package web

import "embed"

// assets contains the templates and static files bundled into the binary.
//
//go:embed templates/*.html static/*
var assets embed.FS
