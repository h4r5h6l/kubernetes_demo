// Package migrations embeds the SQL migration files into the binary.
package migrations

import "embed"

// FS contains every migration in filename order.
//
//go:embed *.sql
var FS embed.FS
