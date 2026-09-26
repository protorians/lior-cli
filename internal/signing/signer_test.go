package signing

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/protorians/lior-cli/internal/pkg"
)

// newVolatileStore returns a fallback key store isolated in a temp dir with a
// fresh random secret.
func newVolatileStore(t *testing.T) *fallbackKeyStore {
	t.Helper()
	dir := t.TempDir()
	secret, err := pkg.NewRandomKey()
	if err != nil {
		t.Fatalf("NewRandomKey: %v", err)
	}
	return &fallbackKeyStore{
		path:   filepath.Join(dir, "signing.enc"),
		secret: secret,
	}
}

func TestGenerateKeyPair(t *testing.T) {
	pub, priv, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair() erroreur inattendue : %v", err)
	}
	if len(pub) != 32 {
		t.Fatalf("taille de clé publique inattendue : %d", len(pub))
	}
	if len(priv) != 64 {
		t.Fatalf("taille de clé privée inattendue : %d", len(priv))
	}
}

func TestFingerprint(t *testing.T) {
	pub1, _, _ := GenerateKeyPair()
	pub2, _, _ := GenerateKeyPair()

	fp1 := Fingerprint(pub1)
	fp2 := Fingerprint(pub2)

	if len(fp1) != 64 {
		t.Fatalf("fingerprint devrait faire 64 caractères hex, obtenu %d", len(fp1))
	}
	if fp1 == fp2 {
		t.Fatal("deux clés différentes ne doivent pas partager le même fingerprint")
	}
}

func TestKeyStoreRoundTrip(t *testing.T) {
	store := newVolatileStore(t)
	if store.HasKeys() {
		t.Fatal("le store neuf ne devrait pas contenir de clés")
	}

	pub, priv, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair() erreur inattendue : %v", err)
	}

	if err := SaveKeyPair(store, pub, priv); err != nil {
		t.Fatalf("SaveKeyPair() erreur inattendue : %v", err)
	}
	if !store.HasKeys() {
		t.Fatal("le store devrait contenir des clés après sauvegarde")
	}

	gotPub, gotPriv, err := LoadKeyPair(store)
	if err != nil {
		t.Fatalf("LoadKeyPair() erreur inattendue : %v", err)
	}
	if !gotPub.Equal(pub) {
		t.Fatal("la clé publique chargée ne correspond pas")
	}
	if !gotPriv.Equal(priv) {
		t.Fatal("la clé privée chargée ne correspond pas")
	}

	if err := store.DeleteKeys(); err != nil {
		t.Fatalf("DeleteKeys() erreur inattendue : %v", err)
	}
	if store.HasKeys() {
		t.Fatal("le store ne devrait plus contenir de clés après suppression")
	}
}

func TestSignAndVerifyArchive(t *testing.T) {
	store := newVolatileStore(t)
	pub, priv, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair() erreur inattendue : %v", err)
	}
	if err := SaveKeyPair(store, pub, priv); err != nil {
		t.Fatalf("SaveKeyPair() erreur inattendue : %v", err)
	}

	dir := t.TempDir()
	archive := filepath.Join(dir, "blog-manager-0.1.0.SenMod")
	if err := os.WriteFile(archive, []byte("contenu de l'archive .SenMod"), 0o600); err != nil {
		t.Fatalf("écriture de l'archive impossible : %v", err)
	}

	sigPath, err := SignArchive(archive, priv)
	if err != nil {
		t.Fatalf("SignArchive() erreur inattendue : %v", err)
	}
	if sigPath != archive+".sig" {
		t.Fatalf("chemin de signature inattendu : %s", sigPath)
	}

	sigData, err := os.ReadFile(sigPath)
	if err != nil {
		t.Fatalf("lecture de la signature impossible : %v", err)
	}
	if len(sigData) != 64 {
		t.Fatalf("une signature Ed25519 doit faire 64 octets, obtenu %d", len(sigData))
	}

	valid, err := VerifySignature(archive, sigPath, pub)
	if err != nil {
		t.Fatalf("VerifySignature() erreur inattendue : %v", err)
	}
	if !valid {
		t.Fatal("la signature devrait être valide")
	}

	if err := os.WriteFile(archive, []byte("archive modifiée après signature"), 0o600); err != nil {
		t.Fatalf("modification de l'archive impossible : %v", err)
	}
	valid, err = VerifySignature(archive, sigPath, pub)
	if err != nil {
		t.Fatalf("VerifySignature() erreur inattendue : %v", err)
	}
	if valid {
		t.Fatal("la signature ne devrait plus être valide après modification de l'archive")
	}
}

func TestLoadPrivateKeyFileFormats(t *testing.T) {
	pub, priv, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	seed := []byte(priv.Seed())

	write := func(content []byte) string {
		p := filepath.Join(t.TempDir(), "key")
		if err := os.WriteFile(p, content, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}

	// A 64-char hex seed must decode to the seed, not be mistaken for a raw
	// 64-byte private key.
	hexSeed := []byte(hex.EncodeToString(seed))
	loaded, err := LoadPrivateKeyFile(write(hexSeed))
	if err != nil {
		t.Fatalf("hex seed: %v", err)
	}
	if !loaded.Public().(ed25519.PublicKey).Equal(pub) {
		t.Error("hex seed loads a different key pair")
	}
	// Raw 64-byte private key.
	loaded, err = LoadPrivateKeyFile(write([]byte(priv)))
	if err != nil {
		t.Fatalf("raw key: %v", err)
	}
	if string(loaded) != string(priv) {
		t.Error("raw private key not preserved")
	}
	// Garbage is refused.
	if _, err := LoadPrivateKeyFile(write([]byte("not-a-key"))); err == nil {
		t.Error("garbage key file must be refused")
	}
}

func TestCanonicalPayloadDeterministic(t *testing.T) {
	p := CanonicalPayload{
		ModuleIdentifier: "mod.acme.demo", Version: "1.3.0",
		Checksum: "aa", ManifestChecksum: "bb", Entry: "index.tsx", Type: "WEB_APP_LOCAL",
	}
	a, b := CanonicalPayloadBytes(p), CanonicalPayloadBytes(p)
	if string(a) != string(b) {
		t.Error("canonical payload encoding is not deterministic")
	}

	_, priv, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	sig := SignPayload(a, priv)
	pub := priv.Public().(ed25519.PublicKey)
	if !VerifyPayload(a, sig, pub) {
		t.Error("produced signature does not verify")
	}
	sig[0] ^= 0xff
	if VerifyPayload(a, sig, pub) {
		t.Error("tampered signature must not verify")
	}
	var raw map[string]any
	if err := json.Unmarshal(a, &raw); err != nil {
		t.Errorf("canonical payload is not valid JSON: %v", err)
	}
}

// TestCanonicalPayloadMatchesServerContract pins the byte-level signature
// contract shared with `buildSignedArtifactPayload`
// (api-resources/module-artifact-crypto.util): keys sorted alphabetically,
// no whitespace. Go struct-order json.Marshal would emit
// {"moduleIdentifier":...,"version":...} and invalidate every signature.
func TestCanonicalPayloadMatchesServerContract(t *testing.T) {
	payload := CanonicalPayloadBytes(CanonicalPayload{
		ModuleIdentifier: "mod.acme.crm",
		Version:          "1.0.0",
		Checksum:         "abc",
		ManifestChecksum: "def",
		Entry:            "index.html",
		Type:             "WEB_APP_REMOTE",
	})
	want := `{"checksum":"abc","entry":"index.html","manifestChecksum":"def","moduleIdentifier":"mod.acme.crm","type":"WEB_APP_REMOTE","version":"1.0.0"}`
	if string(payload) != want {
		t.Fatalf("payload diverges from the server contract:\n got: %s\nwant: %s", payload, want)
	}
}

func TestPublicKeyPEMIsValidSPKI(t *testing.T) {
	pub, priv, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	pemKey, err := PublicKeyPEM(pub)
	if err != nil {
		t.Fatalf("PublicKeyPEM: %v", err)
	}
	if !strings.Contains(pemKey, "-----BEGIN PUBLIC KEY-----") {
		t.Fatalf("expected an SPKI PEM block, got: %s", pemKey)
	}
	// The exported key must verify a signature made with the private key.
	payload := CanonicalPayloadBytes(CanonicalPayload{ModuleIdentifier: "m"})
	if !VerifyPayload(payload, SignPayload(payload, priv), pub) {
		t.Fatal("round-trip verification failed")
	}
}
