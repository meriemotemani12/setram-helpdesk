// Package web embarque l'interface utilisateur (HTML/CSS/JS) dans le binaire.
package web

import (
	"embed"
	"io/fs"
)

//go:embed static
var content embed.FS

// Static renvoie le contenu du dossier static/ à la racine du système de fichiers.
func Static() (fs.FS, error) {
	return fs.Sub(content, "static")
}
