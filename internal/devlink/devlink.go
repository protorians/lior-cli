// Lien de développement module ↔ socle (`<module>/.liorian/dev.json`).
//
// `bind:socle` écrit ce fichier à la racine du module : il y consigne le socle
// visé, son schéma d'URL et le port du dev-server. C'est ce qui permet au
// dev-server de servir le module dans le même contexte que le socle (HTTPS
// compris) et à `liora doctor` de diagnostiquer la boucle de développement.
// Le fichier vit dans `.liorian/`, jamais versionné : il décrit une machine,
// pas un module.
package devlink

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// File est le chemin du lien de développement, relatif à la racine du module.
const File = ".liorian/dev.json"

// Link est le contenu du lien de développement.
type Link struct {
	// SocleDir est le dossier du socle lié (absolu).
	SocleDir string `json:"socleDir"`
	// SocleScheme détermine celui du dev-server.
	SocleScheme string `json:"socleScheme"`
	// LibraryPort est le port du serveur de bibliothèque du socle (0 s'il
	// n'en sert pas).
	LibraryPort int `json:"libraryPort"`
	// DevHost et DevPort sont l'interface et le port attendus du dev-server.
	DevHost string `json:"devHost"`
	DevPort int    `json:"devPort"`
	// BoundAt est la date de la liaison (ISO 8601).
	BoundAt string `json:"boundAt"`
}

// Read lit le lien de développement du module ; ok est faux quand il n'existe
// pas ou est illisible.
func Read(moduleDir string) (Link, bool) {
	data, err := os.ReadFile(filepath.Join(moduleDir, File))
	if err != nil {
		return Link{}, false
	}
	var link Link
	if err := json.Unmarshal(data, &link); err != nil || link.SocleDir == "" {
		return Link{}, false
	}
	return link, true
}

// Write écrit le lien de développement (crée `.liorian/` au besoin) et
// renvoie le chemin écrit.
func Write(moduleDir string, link Link) (string, error) {
	path := filepath.Join(moduleDir, File)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(link, "", "  ")
	if err != nil {
		return "", err
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}
