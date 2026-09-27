// Package localinstall implements `liora install <archive.liozip>`: the
// file-based counterpart of `marketplace install` (spec
// docs/specs/applications/module-isolated-runtime.md §8.4). No catalog and no
// api-core are involved — the archive is audited and validated by the same
// fail-closed engine, then installed multi-version into
// `library/modules/<id>/<version>/` with the `current` pointer.
//
// Trust remains with the marketplace/api-core chain: a local install is a
// developer-loop operation. A `.sig` sidecar is verified against the
// developer keychain when present; a missing or unverifiable signature is a
// warning, never a pass-off as verified.
package localinstall

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/protorians/lior-cli/internal/catalog"
	"github.com/protorians/lior-cli/internal/config"
	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/module"
	"github.com/protorians/lior-cli/internal/signing"
	"github.com/protorians/lior-cli/internal/store"
)

// Installer installs a local `.liozip` archive into a Liora workspace.
type Installer struct {
	Root string
	// Force replaces an already-installed version directory.
	Force bool
}

// Install reads, verifies and installs a local `.liozip` archive.
func (i *Installer) Install(archivePath string) (*catalog.InstallResult, error) {
	data, err := os.ReadFile(archivePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read the archive: %w", err)
	}

	res := &catalog.InstallResult{}
	if err := catalog.InstallArchive(data, i.Root, i.Force, res); err != nil {
		return nil, err
	}

	i.verifySidecar(archivePath, data, res)
	return res, nil
}

// verifySidecar verifies the `.sig` sidecar produced at pack time against the
// developer keychain, when both exist. The outcome is informational: a local
// install never upgrades an unverified archive to "verified".
func (i *Installer) verifySidecar(archivePath string, data []byte, res *catalog.InstallResult) {
	sig, err := os.ReadFile(archivePath + ".sig")
	if err != nil {
		res.SignatureStatus = catalog.SignatureUnsigned
		res.Warnings = append(res.Warnings, i18n.T("localinstall.warning.unsigned"))
		return
	}

	ks := signing.NewKeyStore()
	if !ks.HasKeys() {
		res.SignatureStatus = catalog.SignatureUnverified
		res.Warnings = append(res.Warnings, i18n.T("localinstall.warning.no_key"))
		return
	}
	pub, _, err := signing.LoadKeyPair(ks)
	if err != nil {
		res.SignatureStatus = catalog.SignatureUnverified
		res.Warnings = append(res.Warnings, i18n.T("localinstall.warning.no_key"))
		return
	}

	manifest, err := manifestFromArchive(data)
	if err != nil {
		res.SignatureStatus = catalog.SignatureUnverified
		res.Warnings = append(res.Warnings, i18n.T("localinstall.warning.no_key"))
		return
	}
	payload := signing.CanonicalPayloadBytes(signing.CanonicalPayload{
		ModuleIdentifier: store.CatalogIdentifier("", store.ProductSlug(manifest)),
		Version:          manifest.Version,
		Checksum:         checksumHex(data),
		ManifestChecksum: module.ManifestChecksum(manifest),
		Entry:            manifest.Entry,
		Type:             manifest.Type,
	})
	sigBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	if err != nil || !signing.VerifyPayload(payload, sigBytes, pub) {
		res.SignatureStatus = catalog.SignatureUnverified
		res.Warnings = append(res.Warnings, i18n.T("localinstall.warning.unverified"))
		return
	}
	res.SignatureStatus = catalog.SignatureVerified
}

// manifestFromArchive reads the root manifest.json of a `.liozip` archive.
func manifestFromArchive(data []byte) (*module.Manifest, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("not a valid .liozip archive: %w", err)
	}
	for _, f := range zr.File {
		if filepath.ToSlash(f.Name) != config.ManifestFileName {
			continue
		}
		in, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer in.Close()
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(in); err != nil {
			return nil, err
		}
		var m module.Manifest
		if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
			return nil, err
		}
		return &m, nil
	}
	return nil, errors.New(i18n.T("marketplace.error.manifest"))
}

func checksumHex(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}
