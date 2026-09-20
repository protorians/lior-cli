package signing

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jetbrains/lior-cli/internal/pkg"
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
