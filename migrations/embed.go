// Package migrations embeds the SQL migration files consumed by
// store.RunMigrations. The //go:embed directive must live in this package
// because embed paths resolve relative to the source file's directory,
// which is where the *.sql files live.
package migrations

import "embed"

// FS holds all *.sql migration files at the root of the filesystem.
//
//go:embed *.sql
var FS embed.FS
