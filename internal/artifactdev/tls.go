// Matériau TLS du dev-server (`liora artifact dev`, §8).
//
// Le socle est servi en HTTPS de développement (`next dev
// --experimental-https`, certificats mkcert) : une iframe de module chargée
// depuis `http://localhost:5178` est du mixed content, bloquée
// silencieusement par Chrome. Le dev-server sert donc le certificat du socle
// lié dès qu'il le trouve : même chaîne de confiance, aucune exception à
// poser dans le navigateur.
//
// Ordre de résolution (premier couple complet gagnant) :
//  1. LIORIAN_DEV_TLS_CERT / LIORIAN_DEV_TLS_KEY — chemins explicites,
//  2. le socle lié (`<socle>/certificates/localhost{,-key}.pem`, via
//     `.liorian/dev.json`),
//  3. ~/.config/liorian/certs/ — usage multi-modules,
//  4. <module>/certificates/ — dépôt de module autonome.
package artifactdev

import (
	"os"
	"path/filepath"

	"github.com/protorians/lior-cli/internal/devlink"
)

// MissingCertsHint est le conseil partagé : la marche à suivre pour produire
// les certificats.
const MissingCertsHint = "`mkcert -install && mkcert localhost` dans le dossier du socle (certificates/), " +
	"ou LIORIAN_DEV_TLS_CERT/LIORIAN_DEV_TLS_KEY pointant un couple déjà émis"

// TLS est le couple certificat/clé chargé en mémoire.
type TLS struct {
	Cert     []byte
	Key      []byte
	CertPath string
	KeyPath  string
	// Source est l'origine du couple, pour le journal (`socle lié`, `certs
	// globaux`…).
	Source string
}

// IsTLSRequested décide si le TLS est demandé : `--https` l'impose, `--http`
// l'interdit, sinon la liaison au socle tranche (un socle HTTPS impose HTTPS
// au module). L'environnement LIORIAN_DEV_HTTPS sert d'équivalent pour les
// scripts package.json.
func IsTLSRequested(requested *bool, socleScheme string) bool {
	if requested != nil {
		return *requested
	}
	switch os.Getenv("LIORIAN_DEV_HTTPS") {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return socleScheme == "https"
}

// ResolveTLS charge le premier couple de certificats disponible ; nil quand
// aucun n'est trouvé (l'appelant décide du repli — erreur fermée si le TLS
// était requis).
func ResolveTLS(moduleDir string, certPath, keyPath string) *TLS {
	for _, candidate := range tlsCandidates(moduleDir, certPath, keyPath) {
		cert, certErr := os.ReadFile(candidate.certPath)
		key, keyErr := os.ReadFile(candidate.keyPath)
		if certErr != nil || keyErr != nil || len(cert) == 0 || len(key) == 0 {
			continue
		}
		return &TLS{
			Cert:     cert,
			Key:      key,
			CertPath: candidate.certPath,
			KeyPath:  candidate.keyPath,
			Source:   candidate.source,
		}
	}
	return nil
}

type tlsCandidate struct {
	certPath string
	keyPath  string
	source   string
}

func tlsCandidates(moduleDir, certPath, keyPath string) []tlsCandidate {
	if certPath != "" || keyPath != "" {
		cert := certPath
		if cert == "" {
			cert = filepath.Join(filepath.Dir(keyPath), "localhost.pem")
		}
		key := keyPath
		if key == "" {
			key = filepath.Join(filepath.Dir(certPath), "localhost-key.pem")
		}
		return []tlsCandidate{{certPath: cert, keyPath: key, source: "LIORIAN_DEV_TLS_CERT"}}
	}

	var candidates []tlsCandidate
	// Le socle lié en priorité : c'est lui que le navigateur interroge, son
	// certificat est déjà dans la chaîne de confiance du poste.
	if link, ok := devlink.Read(moduleDir); ok {
		candidates = append(candidates, pair(filepath.Join(link.SocleDir, "certificates"), "socle lié"))
	}
	home, err := os.UserHomeDir()
	if err == nil {
		candidates = append(candidates, pair(filepath.Join(home, ".config", "liorian", "certs"), "certs globaux"))
	}
	candidates = append(candidates, pair(filepath.Join(moduleDir, "certificates"), "dossier du module"))
	return candidates
}

func pair(dir, source string) tlsCandidate {
	return tlsCandidate{
		certPath: filepath.Join(dir, "localhost.pem"),
		keyPath:  filepath.Join(dir, "localhost-key.pem"),
		source:   source,
	}
}
