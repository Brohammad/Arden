package db

import "embed"

// Migrations holds the SQL files executed by arden migrate.
//
//go:embed migrations/*.sql
var Migrations embed.FS
