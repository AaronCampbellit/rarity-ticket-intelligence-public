package migrations

import "embed"

// FS contains the versioned SQL schema used by application startup and tests.
//
//go:embed *.sql
var FS embed.FS
