package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testPEM   = "-----BEGIN PUBLIC KEY-----\nMFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEtest\n-----END PUBLIC KEY-----\n"
	testOther = "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"
	testKeyID = "4d3ab051fccb32e6ff3254a5ac78cea0809c3882423cda74e80ae33b628ee42d"
)

func withTrustFormat(t *testing.T, format string) {
	t.Helper()
	previous := signTrustFormat
	signTrustFormat = format
	t.Cleanup(func() { signTrustFormat = previous })
}

func TestTrustOutputFormats(t *testing.T) {
	ring, err := trustRingJSON(map[string]string{testKeyID: testPEM})
	if err != nil {
		t.Fatalf("trustRingJSON: %v", err)
	}
	for _, tc := range []struct{ format, want string }{
		{"env", "export NEXT_PUBLIC_MODULE_TRUST_KEYS='" + ring + "'\n"},
		{"", "export NEXT_PUBLIC_MODULE_TRUST_KEYS='" + ring + "'\n"},
		{"json", ring + "\n"},
		{"pem", testPEM},
		{"keyid", testKeyID + "\n"},
		{"fingerprint", testKeyID + "\n"},
		{"JSON", ring + "\n"}, // le format est insensible à la casse
	} {
		withTrustFormat(t, tc.format)
		got, oerr := trustOutput(ring, testPEM, testKeyID)
		if oerr != nil {
			t.Errorf("trustOutput(%q) erreur inattendue: %v", tc.format, oerr)
			continue
		}
		if got != tc.want {
			t.Errorf("trustOutput(%q) =\n%q\nwant\n%q", tc.format, got, tc.want)
		}
	}
}

func TestTrustOutputRejectsUnknownFormat(t *testing.T) {
	ring, err := trustRingJSON(map[string]string{testKeyID: testPEM})
	if err != nil {
		t.Fatalf("trustRingJSON: %v", err)
	}
	// Un format inconnu doit échouer, pas produire une sortie vide que le shell
	// lirait comme un trousseau vide.
	withTrustFormat(t, "yaml")
	if _, err := trustOutput(ring, testPEM, testKeyID); err == nil {
		t.Error("trustOutput(\"yaml\") doit échouer")
	}
}

func TestTrustOutputEnvIsSourceableByShell(t *testing.T) {
	ring, err := trustRingJSON(map[string]string{testKeyID: testPEM})
	if err != nil {
		t.Fatalf("trustRingJSON: %v", err)
	}
	withTrustFormat(t, "env")
	out, err := trustOutput(ring, testPEM, testKeyID)
	if err != nil {
		t.Fatalf("trustOutput: %v", err)
	}
	// `eval` de la sortie doit produire une variable relisible par le socle :
	// c'est le chemin documenté (`eval "$(liora sign trust)"`).
	assign := strings.TrimPrefix(out, "export ")
	value, _, found := strings.Cut(assign, "=")
	if !found || value != "NEXT_PUBLIC_MODULE_TRUST_KEYS" {
		t.Fatalf("sortie env inattendue: %q", out)
	}
	decoded := strings.Trim(strings.TrimSpace(strings.SplitN(assign, "=", 2)[1]), `'"`)
	parsed := map[string]string{}
	if err := json.Unmarshal([]byte(decoded), &parsed); err != nil {
		t.Fatalf("la valeur exportée n'est pas un JSON: %v", err)
	}
	if len(parsed) != 1 || !strings.Contains(parsed[testKeyID], "BEGIN PUBLIC KEY") {
		t.Errorf("trousseau exporté inutilisable par le socle: %v", parsed)
	}
}

func TestLoadTrustRingReadsJSONAndEnvFile(t *testing.T) {
	ring, err := trustRingJSON(map[string]string{testKeyID: testPEM})
	if err != nil {
		t.Fatalf("trustRingJSON: %v", err)
	}
	dir := t.TempDir()

	jsonFile := filepath.Join(dir, "trust.json")
	if err := os.WriteFile(jsonFile, []byte(ring+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	envFile := filepath.Join(dir, ".env")
	if err := os.WriteFile(envFile, []byte("# trousseau\nNEXT_PUBLIC_MODULE_TRUST_KEYS='"+ring+"'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{jsonFile, envFile} {
		loaded, lerr := loadTrustRing(path)
		if lerr != nil {
			t.Fatalf("loadTrustRing(%s): %v", path, lerr)
		}
		if len(loaded) != 1 || loaded[testKeyID] != testPEM {
			t.Errorf("loadTrustRing(%s) = %v, want {%s: <PEM>}", filepath.Base(path), loaded, testKeyID)
		}
	}
}

func TestLoadTrustRingDropsEntriesTheSocleIgnores(t *testing.T) {
	// `parseModuleTrustKeys` du socle ne retient que les blocs « PUBLIC KEY » :
	// une entrée parasite dans le fichier était jusqu'ici réécrite telle quelle
	// par `--merge`, produisant un trousseau apparemment peuplé mais vide côté
	// socle.
	dir := t.TempDir()
	path := filepath.Join(dir, "trust.json")
	raw, err := json.Marshal(map[string]string{
		testKeyID: testPEM,
		"cert":    testOther,
		"vide":    "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadTrustRing(path)
	if err != nil {
		t.Fatalf("loadTrustRing: %v", err)
	}
	if len(loaded) != 1 || loaded[testKeyID] != testPEM {
		t.Errorf("loadTrustRing a conservé des entrées inutilisables: %v", loaded)
	}
}

func TestLoadTrustRingRejectsMalformedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.json")
	if err := os.WriteFile(path, []byte("{pas du json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadTrustRing(path); err == nil {
		t.Error("loadTrustRing doit échouer sur un fichier illisible")
	}
	if _, err := loadTrustRing(filepath.Join(dir, "absent.json")); err == nil {
		t.Error("loadTrustRing doit échouer sur un fichier absent")
	}
}

func TestTrustRingJSONIsDeterministic(t *testing.T) {
	// Le fichier est committé côté socle : deux exécutions doivent produire les
	// mêmes octets, sinon chaque export réécrit le même diff.
	ring := map[string]string{
		"b" + testKeyID: testPEM,
		testKeyID:       testPEM,
		"a" + testKeyID: testPEM,
	}
	first, err := trustRingJSON(ring)
	if err != nil {
		t.Fatal(err)
	}
	for range 10 {
		again, aerr := trustRingJSON(ring)
		if aerr != nil {
			t.Fatal(aerr)
		}
		if again != first {
			t.Fatalf("trustRingJSON non déterministe:\n%s\n%s", first, again)
		}
	}
	parsed := map[string]string{}
	if err := json.Unmarshal([]byte(first), &parsed); err != nil {
		t.Fatalf("sortie non JSON: %v", err)
	}
	if len(parsed) != len(ring) {
		t.Errorf("clés perdues: %d != %d", len(parsed), len(ring))
	}
}
