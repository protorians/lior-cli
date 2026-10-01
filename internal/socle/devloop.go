package socle

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Reachability est l'état d'une sonde réseau sur une URL du socle.
type Reachability struct {
	// URL sondée.
	URL string
	// OK vaut vrai si la réponse est un succès HTTP.
	OK bool
	// Status est le code HTTP obtenu, 0 si la connexion a échoué.
	Status int
	// Error décrit l'échec de connexion, chaîne vide si la sonde a abouti.
	Error string
	// Duration est le temps de réponse.
	Duration time.Duration
	// Body est le corps de la réponse, borné à 256 Ko — il ne sert qu'à lire
	// l'index du registre d'installation.
	Body string
}

// LibraryIndex est l'entrée du registre d'installation lue sur le serveur de
// bibliothèque — la preuve que le socle verra le module.
type LibraryIndex struct {
	// SelfServed vaut vrai quand le socle sert lui-même `/library/**` : il n'y a
	// alors rien à sonder, et son absence de réponse n'est pas un défaut.
	SelfServed bool
	// Reachable vaut vrai si l'index a été lu.
	Reachable bool
	// Modules sont les identifiants que le socle découvrira.
	Modules []string
	// Error décrit pourquoi l'index n'a pas pu être lu.
	Error string
}

// ProbeLibrary interroge l'index d'installation du serveur de bibliothèque.
//
// Un index vide n'est pas une erreur : c'est l'état normal d'un socle neuf.
// L'échec de connexion en revanche est un diagnostic — sous `next dev`,
// `/library/**` répond 404, le registre reste vide, et rien ne le signale.
func ProbeLibrary(profile Profile) LibraryIndex {
	if profile.LibraryURL == "" {
		return LibraryIndex{
			SelfServed: true,
			Error: fmt.Sprintf("bibliothèque servie par le socle lui-même (%s) — rien à sonder",
				serverLabel(profile)),
		}
	}
	probe, err := Probe(profile.LibraryURL + "/modules")
	if err != nil {
		return LibraryIndex{Error: fmt.Sprintf("%s — %s", probe.URL, err)}
	}
	return LibraryIndex{Reachable: true, Modules: parseModuleIDs(probe.Body)}
}

// Probe does one HTTP(S) GET against url with a short timeout.
//
// Self-signed development certificates are accepted: they are issued locally
// by mkcert, and the question here is reachability — the browser validates the
// chain itself.
func Probe(url string) (Reachability, error) {
	client := &http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
			DisableKeepAlives: true,
		},
	}
	started := time.Now()
	response, err := client.Get(url)
	result := Reachability{URL: url, Duration: time.Since(started)}
	if err != nil {
		result.Error = err.Error()
		return result, err
	}
	defer response.Body.Close()
	result.Status = response.StatusCode
	result.OK = response.StatusCode >= 200 && response.StatusCode < 300
	if !result.OK {
		result.Error = fmt.Sprintf("HTTP %d", response.StatusCode)
		return result, errors.New(result.Error)
	}
	body := make([]byte, 256*1024)
	read, _ := response.Body.Read(body)
	result.Body = string(body[:read])
	return result, nil
}

// parseModuleIDs extrait les identifiants de modules de l'index d'installation.
//
// Le socle sert un **listing de répertoires** — `[{ "name": "accounting",
// "type": "directory" }]` (`serve.mjs`, `createLibraryHandler`) — dont le SDK
// tire les slugs. Les entrées portant un `type` autre que `directory` ne sont
// pas des modules (fichiers, archives) et sont ignorées, exactement comme le
// fait `hydrateFromLocalLibrary`.
func parseModuleIDs(body string) []string {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return nil
	}
	var entries []struct {
		Name string `json:"name"`
		Type string `json:"type"`
		Id   string `json:"id"`
	}
	if err := json.Unmarshal([]byte(trimmed), &entries); err != nil {
		return nil
	}
	out := []string{}
	for _, entry := range entries {
		if entry.Type != "" && entry.Type != "directory" {
			continue
		}
		out = appendUnique(out, firstNonEmpty(entry.Name, entry.Id))
	}
	return out
}

// serverLabel nomme le serveur du socle, pour un diagnostic lisible.
func serverLabel(profile Profile) string {
	if profile.DevServer {
		return "next dev"
	}
	return "serve.mjs"
}

func appendUnique(values []string, candidate string) []string {
	if candidate == "" {
		return values
	}
	for _, value := range values {
		if value == candidate {
			return values
		}
	}
	return append(values, candidate)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
