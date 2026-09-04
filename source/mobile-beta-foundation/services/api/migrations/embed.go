// Package migrations embeds the SQL migration pairs so a single binary can
// apply them at start-up or through the `migrate` subcommand.
package migrations

import "embed"

// FS holds every NNNN_name.{up,down}.sql file in this directory.
//
//go:embed *.sql
var FS embed.FS
