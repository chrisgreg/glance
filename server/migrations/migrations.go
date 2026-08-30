// Package migrations holds the SQL schema migrations, embedded into the binary.
package migrations

import "embed"

// FS contains every *.sql migration, applied in lexical filename order.
//
//go:embed *.sql
var FS embed.FS
