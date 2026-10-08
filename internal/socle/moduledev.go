// Registre des modules en développement d'un socle —
// `<socle>/.liorian/module.dev.json`.
//
// `liora artifact bind:socle` y inscrit le chemin absolu du module lié : le
// fichier décrit la liste complète des modules en développement du socle.
// C'est ce registre que `liora socle dev` (et `bun run dev` du socle) héberge
// dans son serveur de développement central — un seul serveur, un seul
// terminal, au lieu d'un dev-server par module.
//
// Le fichier vit dans `.liorian/` du socle : non versionné, il décrit une
// machine de développement, pas le dépôt du socle.
package socle

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ModuleDevFile est le registre des modules en développement, relatif à la
// racine du socle.
const ModuleDevFile = ".liorian/module.dev.json"

// ModuleDevEntry décrit un module lié au socle et hébergé par le serveur de
// développement central.
type ModuleDevEntry struct {
	// Identifier est l'identifiant du module (`manifest.id`).
	Identifier string `json:"identifier"`
	// Dir est la racine du module, chemin absolu.
	Dir string `json:"dir"`
	// BoundAt est la date de la liaison (ISO 8601).
	BoundAt string `json:"boundAt"`
}

// moduleDevRegistry est la forme du fichier : la liste des modules en
// développement, stablement triée pour des diffs lisibles.
type moduleDevRegistry struct {
	Modules []ModuleDevEntry `json:"modules"`
}

// ModuleDevPath renvoie le chemin du registre pour un socle.
func ModuleDevPath(socleDir string) string {
	return filepath.Join(socleDir, ModuleDevFile)
}

// ReadModuleDev lit le registre des modules en développement. Un fichier
// absent (socle jamais lié) rend `(nil, nil)` : l'absence d'état est un état
// normal, pas une erreur. Un contenu illisible, en revanche, remonte une
// erreur — un registre corrompu ne doit jamais être écrasé en silence.
func ReadModuleDev(socleDir string) ([]ModuleDevEntry, error) {
	data, err := os.ReadFile(ModuleDevPath(socleDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var registry moduleDevRegistry
	if err := json.Unmarshal(data, &registry); err != nil {
		return nil, fmt.Errorf("%s illisible : %w", ModuleDevFile, err)
	}
	entries := make([]ModuleDevEntry, 0, len(registry.Modules))
	for _, entry := range registry.Modules {
		dir := strings.TrimSpace(entry.Dir)
		if dir == "" {
			continue
		}
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs
		}
		entry.Dir = dir
		entries = append(entries, entry)
	}
	return entries, nil
}

// WriteModuleDev écrit le registre : chemins absolus, entrées triées (par
// identifiant puis dossier) pour que deux machines ou deux runs produisent le
// même fichier.
func WriteModuleDev(socleDir string, entries []ModuleDevEntry) error {
	path := ModuleDevPath(socleDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	normalized := make([]ModuleDevEntry, 0, len(entries))
	for _, entry := range entries {
		dir := strings.TrimSpace(entry.Dir)
		if dir == "" {
			continue
		}
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs
		}
		normalized = append(normalized, ModuleDevEntry{
			Identifier: strings.TrimSpace(entry.Identifier),
			Dir:        dir,
			BoundAt:    entry.BoundAt,
		})
	}
	sort.SliceStable(normalized, func(i, j int) bool {
		if normalized[i].Identifier != normalized[j].Identifier {
			return normalized[i].Identifier < normalized[j].Identifier
		}
		return normalized[i].Dir < normalized[j].Dir
	})
	data, err := json.MarshalIndent(moduleDevRegistry{Modules: normalized}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// UpsertModuleDev ajoute l'entrée du module au registre — ou remplace
// l'entrée déjà présente (même dossier, ou même identifiant : un module
// déplacé ne doit pas laisser de doublon fantôme) — et renvoie le chemin
// écrit. Idempotent : un second bind ne change rien d'autre que `boundAt`.
func UpsertModuleDev(socleDir string, entry ModuleDevEntry) (string, error) {
	entries, err := ReadModuleDev(socleDir)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(entry.BoundAt) == "" {
		entry.BoundAt = time.Now().UTC().Format(time.RFC3339)
	}
	dir := strings.TrimSpace(entry.Dir)
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	identifier := strings.TrimSpace(entry.Identifier)

	kept := make([]ModuleDevEntry, 0, len(entries)+1)
	for _, existing := range entries {
		sameDir := existing.Dir == dir
		sameIdentifier := identifier != "" && existing.Identifier == identifier
		if sameDir || sameIdentifier {
			continue
		}
		kept = append(kept, existing)
	}
	kept = append(kept, ModuleDevEntry{
		Identifier: identifier,
		Dir:        dir,
		BoundAt:    entry.BoundAt,
	})
	if err := WriteModuleDev(socleDir, kept); err != nil {
		return "", err
	}
	return ModuleDevPath(socleDir), nil
}

// RemoveModuleDev retire du registre l'entrée désignée par le dossier du
// module — ou par son identifiant quand le dossier a bougé (`UpsertModuleDev`
// ne conserve qu'une entrée par identifiant, la suppression suit la même
// règle) ; vrai quand le registre a changé. L'absence d'entrée (ni le
// fichier) n'est pas une erreur : `unbind` reste idempotent.
func RemoveModuleDev(socleDir, moduleDir, identifier string) (bool, error) {
	entries, err := ReadModuleDev(socleDir)
	if err != nil {
		return false, err
	}
	if len(entries) == 0 {
		return false, nil
	}
	dir := moduleDir
	if abs, err := filepath.Abs(moduleDir); err == nil {
		dir = abs
	}
	id := strings.TrimSpace(identifier)
	kept := make([]ModuleDevEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Dir == dir || (id != "" && strings.TrimSpace(entry.Identifier) == id) {
			continue
		}
		kept = append(kept, entry)
	}
	if len(kept) == len(entries) {
		return false, nil
	}
	if err := WriteModuleDev(socleDir, kept); err != nil {
		return false, err
	}
	return true, nil
}
