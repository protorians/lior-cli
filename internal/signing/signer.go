package signing

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/protorians/lior-cli/internal/config"
	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/pkg"
)

// GenerateKeyPair generates a new Ed25519 key pair.
func GenerateKeyPair() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate the key pair: %w", err)
	}
	return pub, priv, nil
}

// Fingerprint returns the SHA-256 fingerprint of a public key (hex-encoded).
func Fingerprint(pub ed25519.PublicKey) string {
	hash := sha256.Sum256(pub)
	return hex.EncodeToString(hash[:])
}

// SignArchive signs an archive file (legacy raw-bytes signature) and writes the signature to a .sig file.
func SignArchive(archivePath string, privKey ed25519.PrivateKey) (string, error) {
	data, err := os.ReadFile(archivePath)
	if err != nil {
		return "", fmt.Errorf("failed to read the archive: %w", err)
	}

	sig := ed25519.Sign(privKey, data)

	sigPath := archivePath + ".sig"
	if err := os.WriteFile(sigPath, sig, 0o600); err != nil {
		return "", fmt.Errorf("failed to write the signature: %w", err)
	}

	return sigPath, nil
}

// VerifySignature verifies a .sig file against an archive (legacy raw-bytes signature).
func VerifySignature(archivePath, sigPath string, pubKey ed25519.PublicKey) (bool, error) {
	archiveData, err := os.ReadFile(archivePath)
	if err != nil {
		return false, fmt.Errorf("failed to read the archive: %w", err)
	}

	sigData, err := os.ReadFile(sigPath)
	if err != nil {
		return false, fmt.Errorf("failed to read the signature: %w", err)
	}

	return ed25519.Verify(pubKey, archiveData, sigData), nil
}

// LoadKeyPair loads a public and private key from the key store.
func LoadKeyPair(store KeyStore) (ed25519.PublicKey, ed25519.PrivateKey, error) {
	privBytes, err := store.GetPrivateKey()
	if err != nil {
		return nil, nil, fmt.Errorf("private key not found: %w", err)
	}
	pubBytes, err := store.GetPublicKey()
	if err != nil {
		return nil, nil, fmt.Errorf("public key not found: %w", err)
	}

	privKey := ed25519.PrivateKey(privBytes)
	pubKey := ed25519.PublicKey(pubBytes)

	if !pubKey.Equal(privKey.Public()) {
		return nil, nil, fmt.Errorf("public key and private key do not match")
	}

	return pubKey, privKey, nil
}

// SaveKeyPair saves a public and private key to the key store.
func SaveKeyPair(store KeyStore, pub ed25519.PublicKey, priv ed25519.PrivateKey) error {
	if err := store.SetPublicKey(pub); err != nil {
		return fmt.Errorf("failed to save the public key: %w", err)
	}
	if err := store.SetPrivateKey(priv); err != nil {
		return fmt.Errorf("failed to save the private key: %w", err)
	}
	return nil
}

// CanonicalPayload is the signed statement of a publication (spec
// `module-installation.md` §7.1): the server recomputes it from the received
// bytes and verifies the Ed25519 signature against the developer's public key
// (`DeveloperSigningKey`). Field order is fixed, so the JSON encoding is
// deterministic.
type CanonicalPayload struct {
	ModuleIdentifier string `json:"moduleIdentifier"`
	Version          string `json:"version"`
	Checksum         string `json:"checksum"`
	ManifestChecksum string `json:"manifestChecksum"`
	Entry            string `json:"entry"`
	Type             string `json:"type"`
}

// CanonicalPayloadBytes serializes a signing payload with the shared
// canonical-JSON contract (object keys sorted recursively, no whitespace, no
// HTML escaping) — byte-identical to `buildSignedArtifactPayload` in
// `module-artifact-crypto.util` on the server. Go's struct-order `json.Marshal`
// would emit a different byte stream and invalidate every signature.
func CanonicalPayloadBytes(p CanonicalPayload) []byte {
	data, err := pkg.CanonicalJSON(p)
	if err != nil {
		return nil
	}
	return data
}

// PublicKeyPEM exports an Ed25519 public key as a PEM/SPKI block ("PUBLIC
// KEY") — the format `DeveloperSigningKey.publicKey` carries and the server
// verification expects (`createPublicKey({format: 'pem', type: 'spki'})`).
func PublicKeyPEM(pub ed25519.PublicKey) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", fmt.Errorf("failed to marshal the public key: %w", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})), nil
}

// SignPayload signs canonical payload bytes, returning the raw signature.
func SignPayload(payload []byte, privKey ed25519.PrivateKey) []byte {
	return ed25519.Sign(privKey, payload)
}

// VerifyPayload verifies a canonical payload signature.
func VerifyPayload(payload, sig []byte, pubKey ed25519.PublicKey) bool {
	return ed25519.Verify(pubKey, payload, sig)
}

// ArchiveChecksum returns the SHA-256 hex of an archive file (FR-004: the
// integrity value compared to the catalogue `checksum`).
func ArchiveChecksum(archivePath string) (string, error) {
	data, err := os.ReadFile(archivePath)
	if err != nil {
		return "", fmt.Errorf("failed to read the archive: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// ManifestFromArchive extracts the canonical `manifest.json` stored at the
// archive root (spec TECH-002). It returns an error when the archive carries
// no root manifest (legacy `.SenMod` layout).
func ManifestFromArchive(archivePath string) ([]byte, error) {
	data, err := os.ReadFile(archivePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read the archive: %w", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("not a valid module archive: %w", err)
	}
	for _, f := range zr.File {
		if f.Name != "manifest.json" {
			continue
		}
		in, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("failed to open the embedded manifest: %w", err)
		}
		defer in.Close()
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(in); err != nil {
			return nil, fmt.Errorf("failed to read the embedded manifest: %w", err)
		}
		return buf.Bytes(), nil
	}
	return nil, errors.New("archive carries no manifest.json at its root (legacy layout)")
}

// LoadPrivateKeyFile loads an Ed25519 private key from a file: raw 32-byte
// seed or 64-byte private key, optionally hex- or base64-encoded (e.g.
// `liora sign archive.liozip --key ~/.acme/ed25519`).
func LoadPrivateKeyFile(path string) (ed25519.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read the key file: %w", err)
	}
	text := strings.TrimSpace(string(raw))
	// Decoded forms first: a hex/base64 seed file must not be mistaken for
	// a raw key (e.g. 64 hex chars are 64 bytes, the private-key size).
	var candidates [][]byte
	if decoded, err := hex.DecodeString(text); err == nil {
		candidates = append(candidates, decoded)
	}
	if decoded, err := base64.StdEncoding.DecodeString(text); err == nil {
		candidates = append(candidates, decoded)
	}
	if decoded, err := base64.RawStdEncoding.DecodeString(text); err == nil {
		candidates = append(candidates, decoded)
	}
	candidates = append(candidates, raw)
	for _, c := range candidates {
		switch len(c) {
		case ed25519.PrivateKeySize:
			return ed25519.PrivateKey(c), nil
		case ed25519.SeedSize:
			return ed25519.NewKeyFromSeed(c), nil
		}
	}
	return nil, errors.New("unrecognized private key format (want a 32-byte seed or 64-byte key, raw/hex/base64)")
}

// FindArchive finds the archive for a module in `.lorian/build/`: the
// canonical `.liozip` first, then the deprecated legacy extensions.
func FindArchive(root, moduleName, version string) (string, error) {
	buildDir := config.BuildDir(root)
	candidates := []string{filepath.Join(buildDir, moduleName+"-"+version+config.ArchiveExt)}
	for _, ext := range config.LegacyArchiveExts {
		candidates = append(candidates, filepath.Join(buildDir, moduleName+"-"+version+ext))
	}
	for _, path := range candidates {
		if pkg.FileExists(path) {
			return path, nil
		}
	}
	return "", errors.New(i18n.Tf("sign.error.archive_not_found", moduleName+"-"+version+config.ArchiveExt, buildDir))
}
