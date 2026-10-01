package socle

import (
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// EnsureEnvLocalKey inscrit une clé de transport dans le `.env.local` du socle
// et renvoie true si le fichier a changé.
//
// Une clé déjà renseignée est **toujours préservée** : sa valeur peut venir d'un
// reverse proxy, d'un docker-compose ou d'un réglage manuel que la CLI n'a pas
// le droit d'écraser. Seule l'absence (ou une valeur vide) est corrigée.
//
// Le fichier est créé au besoin, en `0600` : il est hors dépôt, et peut
// contenir des valeurs propres à la machine.
func EnsureEnvLocalKey(socleDir, key, value string) (bool, error) {
	envPath := filepath.Join(socleDir, EnvLocalFile)
	data, err := os.ReadFile(envPath)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	prefix := key + "="
	if existing := parseEnv(string(data)); existing[key] != "" {
		return false, nil
	}

	// Une ligne `KEY=` vide déjà présente est complétée en place, sinon la clé
	// est ajoutée en fin de fichier : l'ordre du `.env.local` reste celui que
	// le développeur a écrit.
	lines := splitEnvLines(string(data))
	replaced := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			lines[i] = prefix + value
			replaced = true
			break
		}
	}
	if !replaced {
		lines = append(lines, prefix+value)
	}

	return writeEnvLines(envPath, lines)
}

// splitEnvLines découpe un fichier d'environnement en lignes, en absorbant le
// séparateur final pour ne jamais produire de ligne vide en fin de rendu.
func splitEnvLines(content string) []string {
	trimmed := strings.TrimRight(content, "\n")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

// writeEnvLines écrit un fichier d'environnement en `0600` : il est hors dépôt
// et peut contenir des valeurs propres à la machine.
func writeEnvLines(path string, lines []string) (bool, error) {
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		return false, err
	}
	return true, nil
}

// EnsureEnvLocalDevModules ajoute un identifiant à la liste d'autorisation de
// dev du socle (`NEXT_PUBLIC_DEV_MODULES`) sans écraser la liste existante.
// `*` reste `*`.
func EnsureEnvLocalDevModules(socleDir, identifier string) (bool, error) {
	envPath := filepath.Join(socleDir, EnvLocalFile)
	data, err := os.ReadFile(envPath)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	ids := SplitDevModules(parseEnv(string(data))[KeyDevModules])
	if ids == nil || contains(ids, "*") || contains(ids, identifier) {
		return false, nil
	}
	ids = append(ids, identifier)
	// La liste est réécrite, pas seulement complétée : `EnsureEnvLocalKey`
	// préserve toute valeur existante, ce qui convient aux URL de transport mais
	// pas ici, où la nouvelle valeur **inclut** l'ancienne.
	return writeEnvLines(envPath, upsertEnvLine(splitEnvLines(string(data)), KeyDevModules, strings.Join(ids, ",")))
}

// SplitDevModules parse a comma-separated list of allowed module identifiers.
// It returns nil when the list is absent or empty — the caller distinguishes
// "no list yet" from "a list that no longer contains this module".
func SplitDevModules(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	out := []string{}
	for _, candidate := range strings.Split(value, ",") {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// RemoveEnvLocalDevModules retire un identifiant de la liste d'autorisation de
// dev du socle.
func RemoveEnvLocalDevModules(socleDir, identifier string) (bool, error) {
	envPath := filepath.Join(socleDir, EnvLocalFile)
	data, err := os.ReadFile(envPath)
	if err != nil {
		return false, nil
	}
	ids := SplitDevModules(parseEnv(string(data))[KeyDevModules])
	if ids == nil {
		return false, nil
	}
	kept := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != identifier && id != "*" {
			kept = append(kept, id)
		}
	}
	if len(kept) == len(ids) {
		return false, nil
	}

	// L'URL du dev-server disparaît avec le dernier identifiant autorisé : un
	// `.env.local` qui pointerait encore vers un serveur arrêté ferait échouer
	// le chargement de tout autre module en dev. La liste, elle, est réécrite
	// avec ce qui reste.
	lines := splitEnvLines(string(data))
	if len(kept) == 0 {
		lines = filterEnvLines(lines, func(key string) bool {
			return key == KeyDevModules || key == KeyDevModulesURL
		})
	} else {
		lines = upsertEnvLine(lines, KeyDevModules, strings.Join(kept, ","))
	}
	changed, err := writeEnvLines(envPath, lines)
	if err != nil {
		return false, err
	}
	return changed, nil
}

func parseEnv(content string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		body := strings.TrimSpace(strings.TrimPrefix(trimmed, "export "))
		index := strings.Index(body, "=")
		if index <= 0 {
			continue
		}
		value := strings.TrimSpace(body[index+1:])
		if len(value) >= 2 {
			first, last := value[0], value[len(value)-1]
			if (first == '"' || first == '\'') && last == first {
				value = value[1 : len(value)-1]
			}
		}
		out[strings.TrimSpace(body[:index])] = value
	}
	return out
}

var envAssignment = regexp.MustCompile(`^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=`)

// envKeyOf renvoie la clé d'une ligne d'environnement, chaîne vide pour un
// commentaire, une ligne vide ou un contenu non assigné.
func envKeyOf(line string) string {
	match := envAssignment.FindStringSubmatch(line)
	if match == nil {
		return ""
	}
	return match[1]
}

// filterEnvLines retire les lignes dont la clé est refusée par drop. Les
// commentaires et lignes vides sont conservés : ce sont des choix du
// développeur, pas des assigns obsolètes.
func filterEnvLines(lines []string, drop func(key string) bool) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if key := envKeyOf(line); key != "" && drop(key) {
			continue
		}
		out = append(out, line)
	}
	return out
}

func upsertEnvLine(lines []string, key, value string) []string {
	prefix := key + "="
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			lines[i] = prefix + value
			return lines
		}
	}
	return append(lines, prefix+value)
}

func contains(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

// EnsureCertificateDir provisions the TLS certificates used by the socle in
// development. It writes nothing when mkcert is absent: the caller reports the
// command to run instead of silently degrading to HTTP.
func EnsureCertificateDir(socleDir string, run func(name string, args ...string) error) error {
	profile := ReadProfile(socleDir)
	if profile.HasCertificates {
		return nil
	}
	if err := os.MkdirAll(profile.CertificatesDir, 0o700); err != nil {
		return err
	}
	// `-install` pose la racine de confiance dans le magasin du système (une
	// fois par poste), `localhost` émet le couple utilisé par le socle et le
	// dev-server des modules.
	if err := run("mkcert", "-install"); err != nil {
		return err
	}
	return run("mkcert", "localhost")
}

// CertificateInstructions décrit la marche à suivre quand mkcert est absent —
// l'information la plus utile est la commande, pas l'erreur du binaire.
func CertificateInstructions() []string {
	return []string{
		"installer mkcert (https://github.com/FiloSottile/mkcert)",
		"cd <socle> && MKCERT_ASSUME_YES=1 mkcert -install && mkcert localhost",
	}
}

// PortOccupied reports whether something already listens on the given TCP port
// of the loopback interface. A busy port is the usual cause of a `next dev` that
// starts on another port, silently breaking the URL a developer opened.
func PortOccupied(port int) bool {
	if port <= 0 {
		return false
	}
	// La connexion suffit : le socle n'a pas besoin d'un listener dédié, et
	// ouvrir une connexion TCP est la même information que celle que `lsof`
	// donnerait, sans dépendance externe.
	for _, addr := range []string{"127.0.0.1:" + strconv.Itoa(port), "[::1]:" + strconv.Itoa(port)} {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			conn.Close()
			return true
		}
	}
	return false
}
