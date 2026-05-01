// Package migrationdata embeds non-schema SQL data files (seeds, fixtures)
// into the binary so the scratch-base Docker image can load them without
// filesystem access.
package migrationdata

import "embed"

// FS contains all *.sql data files embedded at compile time.
//
//go:embed *.sql
var FS embed.FS
