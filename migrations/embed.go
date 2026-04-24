// Package migrations embeds all SQL migration files into the binary.
// lifecycle/migrate.go imports this package and uses FS as the golang-migrate source.
package migrations

import "embed"

// FS contains all *.sql migration files embedded at compile time.
// This enables the scratch-base Docker image to run migrations without filesystem access.
//
//go:embed *.sql
var FS embed.FS
