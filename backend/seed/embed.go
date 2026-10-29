package seed

import "embed"

// Files contains the question dataset used to populate SQLite.
//
//go:embed *.json
var Files embed.FS
