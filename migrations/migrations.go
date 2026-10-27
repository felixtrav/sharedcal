// Package migrations embeds the versioned SQL migrations for every dialect.
package migrations

import "embed"

// FS holds sqlite/, postgres/ and mysql/ migration directories.
//
//go:embed sqlite/*.sql postgres/*.sql mysql/*.sql
var FS embed.FS
