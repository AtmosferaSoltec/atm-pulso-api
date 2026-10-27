// Package db embeds the migrations in the binary: the production image does not
// carry the source code, and the migrate service applies them from here.
package db

import "embed"

// Migrations are the files in db/migrations.
//
//go:embed migrations
var Migrations embed.FS
