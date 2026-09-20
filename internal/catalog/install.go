package catalog

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/protorians/lior-cli/internal/config"
	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/module"
	"github.com/protorians/lior-cli/internal/pkg"
	"github.com/protorians/lior-cli/internal/tui"
)

// Signature verification results.
const (
	SignatureVerified   = "verified"
	SignatureUnsigned   = "unsigned"
	SignatureUnverified = "unverified"
)

// InstallResult describes a successful marketplace install.
type InstallResult struct {
	Module          string   // module directory name (manifest domain)
	Version         string   // published version
	SignatureStatus string   // SignatureVerified | SignatureUnsigned | SignatureUnverified
	Files           int      // number of extracted files
	Warnings        []string // non-fatal notices (unsigned archive, …)
}

// Installer downloads, verifies and extracts a third-party module into a
// Liorian workspace (spec §2.4 Future Scope: `marketplace install`).
type Installer struct {
	Root  string
	Force bool
	// Client overrides the catalog client (tests inject a mock server).
	Client *Client
}

func (i *Installer) client() *Client {
	if i.Client != nil {
		return i.Client
	}
	return NewClient()
}

// Install resolves a module in the catalog, downloads its `.SenMod` archive,
// verifies the SHA-256 checksum (and the Ed25519 signature when the publisher
// provides one), then extracts the module in place.
func (i *Installer) Install(ctx context.Context, ref string) (*InstallResult, error) {
	mod, err := i.client().GetModule(ctx, ref)
	if err != nil {
		if pkg.IsNotFound(err) {
			return nil, pkg.NewErrorWithFix(i18n.T("cat.marketplace"),
				err.Error(),
				i18n.T("marketplace.install.not_found.fix"),
				pkg.ExitModuleNotFound)
		}
		return nil, pkg.NewErrorWithFix(i18n.T("cat.marketplace"),
			err.Error(),
			i18n.T("marketplace.error.search.fix"),
			pkg.ExitNetwork)
	}

	name := strings.TrimSpace(mod.Domain)
	if name == "" {
		name = mod.Slug
	}
	if name == "" {
		return nil, pkg.NewError(i18n.T("cat.marketplace"),
			"module has no domain or slug", pkg.ExitError)
	}

	moduleDir := config.ModuleDir(i.Root, name)
	if pkg.DirExists(moduleDir) && !i.Force {
		return nil, pkg.NewErrorWithFix(i18n.T("cat.marketplace"),
			i18n.Tf("marketplace.install.already", name, moduleDir),
			i18n.Tf("marketplace.install.already.fix", name),
			pkg.ExitModuleNotFound)
	}
	if pkg.DirExists(moduleDir) && i.Force && tui.IsInteractive() {
		proceed, cerr := tui.Confirm(i18n.Tf("marketplace.install.confirm", name), false)
		if cerr != nil {
			return nil, cerr
		}
		if !proceed {
			return nil, nil
		}
	}

	archive, err := i.client().DownloadArtifact(ctx, mod.ArtifactURL)
	if err != nil {
		return nil, pkg.NewErrorWithFix(i18n.T("cat.marketplace"),
			err.Error(),
			i18n.T("marketplace.error.download.fix"),
			pkg.ExitNetwork)
	}

	res := &InstallResult{Module: name, Version: mod.Version}

	if err := verifyChecksum(mod.ArtifactChecksum, archive); err != nil {
		return nil, pkg.NewErrorWithFix(i18n.T("cat.marketplace"),
			err.Error(),
			i18n.T("marketplace.error.checksum.fix"),
			pkg.ExitError)
	}

	if err := verifySignature(mod, archive, res); err != nil {
		return nil, err
	}

	if err := unpack(archive, i.Root, i.Force, res); err != nil {
		return nil, pkg.NewError(i18n.T("cat.marketplace"), err.Error(), pkg.ExitError)
	}
	return res, nil
}

// verifyChecksum compares the archive SHA-256 with the catalog checksum. A
// missing catalog checksum is not an error (best-effort verification).
func verifyChecksum(expected string, data []byte) error {
	expected = normalizeChecksum(expected)
	if expected == "" {
		return nil
	}
	got := checksumHex(data)
	if !strings.EqualFold(got, expected) {
		return errors.New(i18n.Tf("marketplace.error.checksum", expected, got))
	}
	return nil
}

// verifySignature verifies the optional Ed25519 signature of an archive.
// An unsigned archive is a warning; a signature without a public key cannot be
// checked (warning); a present-but-invalid signature is a hard error.
func verifySignature(mod *CatalogModule, data []byte, res *InstallResult) error {
	if strings.TrimSpace(mod.Signature) == "" {
		res.SignatureStatus = SignatureUnsigned
		res.Warnings = append(res.Warnings, i18n.T("marketplace.verify.unsigned"))
		return nil
	}
	if strings.TrimSpace(mod.SignaturePublicKey) == "" {
		res.SignatureStatus = SignatureUnverified
		res.Warnings = append(res.Warnings, i18n.T("marketplace.verify.unverified"))
		return nil
	}
	sig, err := base64.StdEncoding.DecodeString(mod.Signature)
	if err != nil {
		return pkg.NewError(i18n.T("cat.signature"),
			i18n.T("marketplace.error.signature"), pkg.ExitSigning)
	}
	pub, err := base64.StdEncoding.DecodeString(mod.SignaturePublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return pkg.NewErrorWithFix(i18n.T("cat.signature"),
			i18n.T("marketplace.error.signature.no_key"),
			i18n.T("marketplace.error.signature.no_key.fix"), pkg.ExitSigning)
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), data, sig) {
		return pkg.NewErrorWithFix(i18n.T("cat.signature"),
			i18n.T("marketplace.error.signature"),
			i18n.T("marketplace.error.signature.fix"), pkg.ExitSigning)
	}
	res.SignatureStatus = SignatureVerified
	return nil
}

// normalizeChecksum strips common prefixes ("sha256:") and trims whitespace.
func normalizeChecksum(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.TrimPrefix(s, "sha256:")
	return strings.TrimSpace(s)
}

func checksumHex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// unpack extracts a `.SenMod` (ZIP) archive into a temporary directory,
// validates the extracted module and copies only this module's directories
// into the workspace: `external_modules/<name>/`, `public/assets/<name>/` and
// `src/app/<uri-or-id>/`. Nothing touches the workspace when the archive is
// invalid or fails validation, so a bad install never leaves partial files.
func unpack(data []byte, root string, force bool, res *InstallResult) error {
	tmp, err := os.MkdirTemp("", "lorian-marketplace-*")
	if err != nil {
		return fmt.Errorf("failed to create a temporary directory: %w", err)
	}
	defer os.RemoveAll(tmp)

	var files []string
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("not a valid .SenMod archive: %w", err)
	}
	for _, f := range zr.File {
		rel := sanitizeEntry(f.Name)
		if rel == "" || !allowedEntry(rel) {
			continue
		}
		target := filepath.Join(tmp, filepath.FromSlash(rel))
		if !withinDir(tmp, target) {
			continue
		}
		if err := writeZipEntry(f, target); err != nil {
			return err
		}
		files = append(files, rel)
	}
	if len(files) == 0 {
		return errors.New(i18n.T("marketplace.error.manifest"))
	}

	name, err := moduleNameFromFiles(files)
	if err != nil {
		return err
	}
	manifest, err := module.LoadManifest(
		filepath.Join(tmp, config.ExternalModulesDir, name, config.ManifestFileName))
	if err != nil {
		return fmt.Errorf("failed to read the module manifest: %w", err)
	}

	vr := &module.Validator{Root: tmp}
	valRes, err := vr.ValidateModule(name)
	if err != nil {
		return err
	}
	if valRes.HasErrors() {
		return errors.New(i18n.Tf("marketplace.error.invalid", name, valRes.ErrorCount()))
	}

	res.Files = len(files)
	if res.Module == "" {
		res.Module = name
	}

	pageDir := strings.TrimPrefix(manifest.URI, "/")
	if pageDir == "" {
		pageDir = manifest.ID
	}
	targets := []string{
		filepath.Join(config.ExternalModulesDir, name),
		filepath.Join(config.PublicAssetsDir, name),
		filepath.Join(config.AppSrcDir, pageDir),
	}
	for _, rel := range targets {
		src := filepath.Join(tmp, filepath.FromSlash(rel))
		if !pkg.DirExists(src) {
			continue
		}
		dst := filepath.Join(root, filepath.FromSlash(rel))
		if pkg.PathExists(dst) && !force {
			continue
		}
		if pkg.PathExists(dst) {
			if err := os.RemoveAll(dst); err != nil {
				return fmt.Errorf("failed to replace %s: %w", dst, err)
			}
		}
		if err := pkg.CopyDir(src, dst); err != nil {
			return fmt.Errorf("failed to copy %s: %w", rel, err)
		}
	}
	return nil
}

// sanitizeEntry normalizes a zip entry name to a clean, project-relative path.
// Absolute paths and `..` traversal are rejected (empty result).
func sanitizeEntry(name string) string {
	clean := strings.TrimPrefix(path.Clean("/"+filepath.ToSlash(name)), "/")
	if clean == "" || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return ""
	}
	return clean
}

// allowedEntry reports whether an archive entry belongs to the module layout.
func allowedEntry(rel string) bool {
	return strings.HasPrefix(rel, "external_modules/") ||
		strings.HasPrefix(rel, "src/") ||
		strings.HasPrefix(rel, "public/")
}

// withinDir reports whether file is inside dir (defense in depth against
// traversal, after the path.Clean pass).
func withinDir(dir, file string) bool {
	rel, err := filepath.Rel(dir, file)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func writeZipEntry(f *zip.File, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	in, err := f.Open()
	if err != nil {
		return fmt.Errorf("failed to open archive entry %q: %w", f.Name, err)
	}
	defer in.Close()

	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("failed to write archive entry %q: %w", f.Name, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("failed to write archive entry %q: %w", f.Name, err)
	}
	return nil
}

// moduleNameFromFiles returns the module directory name, i.e. the `name`
// segment of `external_modules/<name>/manifest.json`.
func moduleNameFromFiles(files []string) (string, error) {
	for _, rel := range files {
		rest, ok := strings.CutPrefix(rel, "external_modules/")
		if !ok {
			continue
		}
		if !strings.HasSuffix(rest, "/"+config.ManifestFileName) {
			continue
		}
		parts := strings.Split(rest, "/")
		if len(parts) >= 2 {
			return parts[0], nil
		}
	}
	return "", errors.New(i18n.T("marketplace.error.manifest"))
}
