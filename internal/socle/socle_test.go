package socle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSocle écrit un socle minimal mais fidèle : `library/`, les scripts `dev`
// et `dev:library`, et l'hôte d'application — les trois signaux dont dépend le
// profil. Un socle sans ces signaux resterait partial, ce qui est précisément
// ce que ces tests vérifient.
func writeSocle(t *testing.T, opts map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "library", "modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	pkg := `{"name":"liorian-socle","scripts":{` +
		`"dev":"bun run dev:library & next dev -p 5010 --experimental-https",` +
		`"dev:library":"node scripts/serve-library.mjs",` +
		`"start":"node serve.mjs"}}`
	for key, value := range opts {
		if key == "package.json" {
			pkg = value
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, key), []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkg), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestReadProfileLitLeSocleDeDev(t *testing.T) {
	dir := writeSocle(t, map[string]string{".env": "NEXT_PUBLIC_APP_HOST=https://localhost:5010\n"})
	profile := ReadProfile(dir)

	if profile.Scheme != "https" {
		t.Errorf("Scheme = %q, want https", profile.Scheme)
	}
	if !profile.DevServer {
		t.Error("DevServer = false, want true (le script dev lance next dev)")
	}
	if profile.AppPort != 5010 {
		t.Errorf("AppPort = %d, want 5010", profile.AppPort)
	}
	if profile.LibraryPort != DefaultLibraryPort {
		t.Errorf("LibraryPort = %d, want %d", profile.LibraryPort, DefaultLibraryPort)
	}
	// La clé manquante : sans elle le registre d'installation reste vide sous
	// `next dev`, et la page affiche « Module introuvable » pour un module
	// pourtant lié. C'est la cause racine du symptôme que l'outillage corrige.
	if profile.LibraryURL != "https://localhost:5011/library" {
		t.Errorf("LibraryURL = %q, want https://localhost:5011/library", profile.LibraryURL)
	}
}

func TestReadProfileSocleDeProductionSansURLDeBibliotheque(t *testing.T) {
	// Un socle servi par `serve.mjs` rend lui-même `/library/**` : inscrire une
	// URL absolue serait faux, et le serait doublement derrière un proxy TLS.
	dir := writeSocle(t, map[string]string{
		"package.json": `{"name":"liorian-socle","scripts":{"dev":"next build","start":"node serve.mjs"}}`,
		".env":         "NEXT_PUBLIC_APP_HOST=https://app.example.com\n",
	})
	profile := ReadProfile(dir)

	if profile.DevServer {
		t.Error("DevServer = true, want false")
	}
	if profile.LibraryURL != "" {
		t.Errorf("LibraryURL = %q, want empty (bibliothèque servie par le socle)", profile.LibraryURL)
	}
}

func TestReadProfileRespecteUnPortDeBibliothequeDeclare(t *testing.T) {
	dir := writeSocle(t, map[string]string{
		"package.json": `{"name":"liorian-socle","scripts":{"dev":"next dev -p 5010 --experimental-https","dev:library":"node scripts/serve-library.mjs --port 5099"}}`,
	})
	if got := ReadProfile(dir).LibraryPort; got != 5099 {
		t.Errorf("LibraryPort = %d, want 5099", got)
	}
}

func TestReadProfileSocleSansSignalEstHTTPS(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "library"), 0o755); err != nil {
		t.Fatal(err)
	}
	profile := ReadProfile(dir)
	// La posture canonique du socle en développement : mieux vaut un avertissement
	// qu'une iframe que le navigateur bloque en silence.
	if profile.Scheme != "https" {
		t.Errorf("Scheme = %q, want https (posture canonique)", profile.Scheme)
	}
	if !IsSocle(dir) {
		t.Error("IsSocle = false, want true (library/ est un marqueur)")
	}
}

func TestIsSocleIgnoreLeNomDuDossier(t *testing.T) {
	// Un dossier temporaire nommé `not-a-socle` n'est pas un socle : le nom du
	// dossier n'est pas un signal, sinon tout dossier temporaire serait accepté.
	dir := filepath.Join(t.TempDir(), "not-a-socle")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if IsSocle(dir) {
		t.Error("IsSocle = true, want false (aucun marqueur de socle)")
	}
}

func TestReadProfileDetecteLesCertificats(t *testing.T) {
	dir := writeSocle(t, map[string]string{})
	if ReadProfile(dir).HasCertificates {
		t.Error("HasCertificates = true alors qu'aucun pem n'est écrit")
	}
	certs := filepath.Join(dir, "certificates")
	if err := os.MkdirAll(certs, 0o700); err != nil {
		t.Fatal(err)
	}
	// Un seul pem ne sert à rien : le couple doit être complet.
	if err := os.WriteFile(filepath.Join(certs, "localhost.pem"), []byte("cert"), 0o600); err != nil {
		t.Fatal(err)
	}
	if ReadProfile(dir).HasCertificates {
		t.Error("HasCertificates = true avec la clé manquante")
	}
	if err := os.WriteFile(filepath.Join(certs, "localhost-key.pem"), []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !ReadProfile(dir).HasCertificates {
		t.Error("HasCertificates = false, want true")
	}
}

func TestEnsureEnvLocalKeyPreserveUneValeurExistante(t *testing.T) {
	dir := writeSocle(t, map[string]string{".env.local": "NEXT_PUBLIC_LIBRARY_MODULES_URL=https://proxy.example/library\n"})
	changed, err := EnsureEnvLocalKey(dir, KeyLibraryModulesURL, "https://localhost:5011/library")
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("EnsureEnvLocalKey a réécrit une valeur existante (reverse proxy, docker)")
	}
	data, _ := os.ReadFile(filepath.Join(dir, ".env.local"))
	if want := "https://proxy.example/library"; !strings.Contains(string(data), want) {
		t.Errorf("valeur existante perdue : %q", string(data))
	}
}

func TestEnsureEnvLocalKeyAjouteUneCleManquante(t *testing.T) {
	dir := writeSocle(t, map[string]string{".env.local": "NEXT_PUBLIC_DEV_MODULES=accounting\n"})
	changed, err := EnsureEnvLocalKey(dir, KeyLibraryModulesURL, "https://localhost:5011/library")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("EnsureEnvLocalKey n'a rien écrit")
	}
	data, _ := os.ReadFile(filepath.Join(dir, ".env.local"))
	if !strings.Contains(string(data), KeyLibraryModulesURL+"=https://localhost:5011/library") {
		t.Errorf("clé absente : %q", string(data))
	}
	if !strings.Contains(string(data), "NEXT_PUBLIC_DEV_MODULES=accounting") {
		t.Error("la clé préexistante a été perdue")
	}
}

func TestEnsureEnvLocalDevModulesN_EcrasePasLaListe(t *testing.T) {
	dir := writeSocle(t, map[string]string{".env.local": "NEXT_PUBLIC_DEV_MODULES=accounting\n"})
	if _, err := EnsureEnvLocalDevModules(dir, "billing"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, ".env.local"))
	if !strings.Contains(string(data), "accounting") || !strings.Contains(string(data), "billing") {
		t.Errorf("liste incohérente : %q", string(data))
	}
	// `*` autorise tout : le明星 ne doit pas être restreint par un ajout.
	star := writeSocle(t, map[string]string{".env.local": "NEXT_PUBLIC_DEV_MODULES=*\n"})
	if _, err := EnsureEnvLocalDevModules(star, "billing"); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(star, ".env.local"))
	if !strings.Contains(string(data), "NEXT_PUBLIC_DEV_MODULES=*") {
		t.Errorf("`*` a été restreint : %q", string(data))
	}
}

func TestRemoveEnvLocalDevModulesRetireLaDerniereEntree(t *testing.T) {
	dir := writeSocle(t, map[string]string{
		".env.local": "NEXT_PUBLIC_DEV_MODULES_URL=https://localhost:5178\nNEXT_PUBLIC_DEV_MODULES=accounting\n",
	})
	if _, err := RemoveEnvLocalDevModules(dir, "accounting"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, ".env.local"))
	// Une URL orpheline ferait échouer le chargement de tout autre module en
	// dev : elle disparaît avec le dernier identifiant autorisé.
	if strings.Contains(string(data), KeyDevModules) || strings.Contains(string(data), KeyDevModulesURL) {
		t.Errorf("des clés de dev sont restées après le retrait du dernier module : %q", string(data))
	}
}

func TestRemoveEnvLocalDevModulesConserveLesAutres(t *testing.T) {
	dir := writeSocle(t, map[string]string{
		".env.local": "NEXT_PUBLIC_DEV_MODULES=accounting,billing\n",
	})
	if _, err := RemoveEnvLocalDevModules(dir, "accounting"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, ".env.local"))
	if strings.Contains(string(data), "accounting") {
		t.Errorf("l'identifiant retiré est resté : %q", string(data))
	}
	if !strings.Contains(string(data), "billing") {
		t.Errorf("l'identifiant conservé a disparu : %q", string(data))
	}
}

func TestReadEnvFileNeLitQueDesAffectations(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "# commentaire\n\nexport NEXT_PUBLIC_APP_HOST=\"https://localhost:5010\"\n" +
		"MALFORMED\nNEXT_PUBLIC_DEV_MODULES=accounting\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	parsed := parseEnv(content)
	if parsed[KeyAppHost] != "https://localhost:5010" {
		t.Errorf("AppHost = %q, want https://localhost:5010 (export + quotes)", parsed[KeyAppHost])
	}
	if _, ok := parsed[""]; ok {
		t.Error("une ligne sans affectation a produit une clé vide")
	}
}

func TestSchemeOfEtHostOf(t *testing.T) {
	cases := []struct {
		url    string
		scheme string
		host   string
		port   int
	}{
		{"https://localhost:5010", "https", "localhost", 5010},
		{"http://host.docker.internal:5178", "http", "host.docker.internal", 5178},
		{"https://app.example.com/library", "https", "app.example.com", 0},
		{"", "", "", 0},
		{"pas une url", "", "", 0},
	}
	for _, c := range cases {
		if got := SchemeOf(c.url); got != c.scheme {
			t.Errorf("SchemeOf(%q) = %q, want %q", c.url, got, c.scheme)
		}
		if got := HostOf(c.url); got != c.host {
			t.Errorf("HostOf(%q) = %q, want %q", c.url, got, c.host)
		}
		if got := PortOf(c.url); got != c.port {
			t.Errorf("PortOf(%q) = %d, want %d", c.url, got, c.port)
		}
	}
}

func TestParseModuleIDsDuListingDeRepertoires(t *testing.T) {
	// Forme réellement servie par `serve.mjs` : un listing de répertoires, dont
	// le SDK tire les slugs. Une entrée qui n'est pas un répertoire n'est pas
	// un module et doit être ignorée, comme le fait `hydrateFromLocalLibrary`.
	body := `[
	  {"name": "accounting", "type": "directory"},
	  {"name": "billing", "type": "directory"},
	  {"name": "notes.txt", "type": "file"}
	]`
	got := parseModuleIDs(body)
	if len(got) != 2 || got[0] != "accounting" || got[1] != "billing" {
		t.Errorf("parseModuleIDs = %v, want [accounting billing]", got)
	}
	if ids := parseModuleIDs("pas du json"); ids != nil {
		t.Errorf("parseModuleIDs(illisible) = %v, want nil", ids)
	}
}
