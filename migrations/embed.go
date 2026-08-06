// Package migrations содержит SQL-миграции PostgreSQL, встроенные в приложение.
package migrations

import "embed"

// Files содержит встроенные SQL-файлы миграций.
//
//go:embed *.sql
var Files embed.FS
