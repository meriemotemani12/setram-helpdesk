// Package migrations embarque les fichiers SQL de migration.
// Le format de nommage {version}_{titre}.up.sql / .down.sql est compatible
// avec l'outil golang-migrate (CLI) si vous préférez l'utiliser.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
