package cmd

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/protorians/lior-cli/internal/auth"
	"github.com/protorians/lior-cli/internal/config"
	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/module"
	"github.com/protorians/lior-cli/internal/pkg"
	"github.com/protorians/lior-cli/internal/signing"
	"github.com/protorians/lior-cli/internal/store"
	"github.com/protorians/lior-cli/internal/tui"
	"github.com/spf13/cobra"
)

var signKeyFile string

var signCmd = &cobra.Command{
	Use:   "sign [module|archive.liozip]",
	Short: "Sign .liozip archives (Ed25519)",
	Long: `Manages Ed25519 digital signatures for modules: generates keys,
binds them to modules, signs the .liozip archives and verifies signatures.

The signature covers the canonical publication payload (module identifier,
version, archive checksum, manifest checksum, entry, type — spec
module-installation §7.1) and is mandatory for publication (ADR-010).

Key lifecycle:
  1. sign keygen                Generate a key pair (OS keychain / encrypted
                                file) and register the public key on the
                                store when connected — otherwise the sync is
                                deferred to the next publish.
  2. sign <module.domain>       Sign the archive and bind the module domain to
                                the key. If another key is already bound, a
                                rotation of the previous one is proposed.
  3. pack / publish             pack signs automatically; publish verifies the
                                signature locally, then the store verifies it
                                again on reception against the ACTIVE key
                                registered for your account.

Subcommands:
   sign keygen                  Generate an Ed25519 key pair
   sign <module|archive>        Sign a module's .liozip archive
   sign verify <module|archive> Verify a module's signature

Without arguments, shows the SHA-256 fingerprint of the public key.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return printSigningFingerprint()
		}
		return signModule(args)
	},
}

func init() {
	signCmd.PersistentFlags().StringVar(&signKeyFile, "key", "", i18n.T("sign.flag.key"))
	i18nFlag(signCmd, "key", "sign.flag.key")
	signCmd.AddCommand(signKeygenCmd, signVerifyCmd)
	i18nHelp(signCmd, "cmd.sign.short", "cmd.sign.long")
	i18nHelp(signKeygenCmd, "cmd.sign.keygen.short", "cmd.sign.keygen.long")
	i18nHelp(signVerifyCmd, "cmd.sign.verify.short", "cmd.sign.verify.long")
}

var signKeygenCmd = &cobra.Command{
	Use:   "keygen",
	Short: "Generate an Ed25519 key pair",
	Long: `Generates an Ed25519 key pair and stores it in the system keychain
(fallback: encrypted file ~/.lorian-cli/signing.enc).

If keys already exist, asks for confirmation before overwriting them
(silent regeneration in non-interactive mode).`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSignKeygen()
	},
}

var signVerifyCmd = &cobra.Command{
	Use:   "verify [module|archive.liozip]",
	Short: "Verify a module's signature",
	Long: `Verifies the validity of a module's .sig file against its
.liozip archive, using the public key stored in the keychain.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return verifySignature(args)
	},
}

// printSigningFingerprint prints the SHA-256 fingerprint of the public key (FR-024).
func printSigningFingerprint() error {
	store := signing.NewKeyStore()
	pub, err := store.GetPublicKey()
	if err != nil {
		return pkg.NewErrorWithFix(i18n.T("cat.signature"),
			i18n.T("sign.error.no_key"),
			i18n.T("sign.error.keygen.fix"),
			pkg.ExitSigning)
	}

	s := tui.NewStyles()
	fmt.Println()
	fmt.Println(s.NeutralPanel(
		s.SubHeader.Render(i18n.T("sign.header")) + "\n\n" +
			s.KeyValue(i18n.T("label.fingerprint"),
				s.Info.Render(signing.Fingerprint(ed25519.PublicKey(pub)))),
	))
	return nil
}

// runSignKeygen generates an Ed25519 key pair and stores it (spec §5.13.1).
func runSignKeygen() error {
	store := signing.NewKeyStore()

	if store.HasKeys() {
		s := tui.NewStyles()
		fmt.Println()
		body := s.Warning.Render(i18n.T("sign.keys_exist"))
		if pub, err := store.GetPublicKey(); err == nil {
			body += "\n\n" + s.KeyValue(i18n.T("label.current_fingerprint"),
				s.Info.Render(signing.Fingerprint(ed25519.PublicKey(pub))))
		}
		fmt.Println(s.WarningPanel(body))
		if tui.IsInteractive() {
			regenerate, err := tui.Confirm(i18n.T("sign.confirm.regenerate"), false)
			if err != nil {
				return err
			}
			if !regenerate {
				return nil
			}
		}
	}

	pub, priv, err := signing.GenerateKeyPair()
	if err != nil {
		return pkg.NewError(i18n.T("cat.signature"), err.Error(), pkg.ExitSigning)
	}

	_, err = tui.RunWithSpinner(i18n.T("sign.spinner.saving"), func() (struct{}, error) {
		return struct{}{}, signing.SaveKeyPair(store, pub, priv)
	})
	if err != nil {
		return pkg.NewError(i18n.T("cat.signature"), err.Error(), pkg.ExitSigning)
	}

	s := tui.NewStyles()
	fmt.Println()
	rows := []string{
		s.KeyValue(i18n.T("label.fingerprint"),
			s.Info.Render(signing.Fingerprint(pub))+" (SHA-256 of the public key)"),
		s.KeyValue(i18n.T("label.private_key"), i18n.T("sign.stored_keychain")),
		s.KeyValue(i18n.T("label.public_key"), i18n.T("sign.stored_keychain")),
	}

	// Sync the new public key with the store right away (spec: "créer une
	// signature avec 'liora sign' → synchroniser avec le serveur, ce qui va
	// permettre de vérifier les futurs artefacts"). Without a session the
	// synchronization is deferred: `liora publish` re-runs it idempotently
	// (EnsureSigningKey) before the upload.
	keyID, synced, serr := syncSigningKeyToServer(pub)
	fmt.Println()
	if serr != nil {
		rows = append(rows, s.KeyValue(i18n.T("label.sync"), s.Error.Render(i18n.Tf("sign.sync.failed", serr.Error()))))
	} else if synced {
		rows = append(rows, s.KeyValue(i18n.T("label.sync"), s.Success.Render(i18n.T("sign.sync.done"))+
			" ("+i18n.T("label.key_id")+" "+s.Info.Render(shortDigest(keyID))+")"))
	} else {
		rows = append(rows, s.KeyValue(i18n.T("label.sync"), s.Warning.Render(i18n.T("sign.sync.deferred"))))
	}
	fmt.Println(s.SummaryCard(
		s.Success.Render(i18n.T("sign.keygen.success")),
		rows...,
	))
	fmt.Println()
	return nil
}

// syncSigningKeyToServer registers the local public key on the developer
// store when an authenticated session exists. It reports whether the
// synchronization happened (false when the CLI is offline/unauthenticated —
// never an error: publish re-syncs idempotently).
func syncSigningKeyToServer(pub ed25519.PublicKey) (keyID string, synced bool, err error) {
	sess, err := auth.LoadSession(auth.NewStore())
	if err != nil || sess == nil || !sess.IsAuthenticated() {
		return "", false, nil
	}
	client := store.NewClient()
	client.SetToken(sess.AccessToken)
	client.WithAutoRefresh(sess)

	pemKey, err := signing.PublicKeyPEM(pub)
	if err != nil {
		return "", false, err
	}
	key, _, err := client.EnsureSigningKey(context.Background(), pemKey)
	if err != nil {
		return "", false, err
	}
	if key != nil {
		return key.KeyID, true, nil
	}
	return "", true, nil
}

// signTarget describes what to sign: a project module or a standalone archive
// file (e.g. `liora sign acme-crm.liozip --key ~/.acme/ed25519`).
type signTarget struct {
	label        string // display name (module domain or archive base)
	archivePath  string
	manifest     *module.Manifest
	manifestJSON []byte // canonical embedded bytes when read from the archive
}

// resolveSignTarget resolves a `sign`/`sign verify` argument to an archive and
// its manifest, from either a module name or an archive file path.
func resolveSignTarget(root, arg string) (*signTarget, error) {
	if isArchivePath(arg) {
		archivePath := arg
		if !filepath.IsAbs(archivePath) && root != "" {
			if pkg.FileExists(archivePath) {
				if abs, err := filepath.Abs(archivePath); err == nil {
					archivePath = abs
				}
			} else {
				candidate := filepath.Join(root, arg)
				if pkg.FileExists(candidate) {
					archivePath = candidate
				}
			}
		}
		raw, err := signing.ManifestFromArchive(archivePath)
		if err != nil {
			return nil, pkg.NewErrorWithFix(i18n.T("cat.signature"),
				err.Error(),
				i18n.T("sign.error.legacy_archive.fix"),
				pkg.ExitSigning)
		}
		var m module.Manifest
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, pkg.NewError(i18n.T("cat.manifest"), err.Error(), pkg.ExitManifest)
		}
		return &signTarget{
			label:        m.Domain,
			archivePath:  archivePath,
			manifest:     &m,
			manifestJSON: raw,
		}, nil
	}

	name := normalizeModuleArg(arg)
	if !pkg.DirExists(filepath.Join(root, config.ExternalModulesDir, name)) {
		return nil, pkg.NewErrorWithFix(
			i18n.T("cat.module"),
			i18n.Tf("modules.error.module_absent", name, config.ExternalModulesDir),
			i18n.T("modules.error.module_absent.fix"),
			pkg.ExitModuleNotFound,
		)
	}
	m, err := module.LoadManifest(config.ManifestPath(root, name))
	if err != nil {
		return nil, pkg.NewError(i18n.T("cat.manifest"), err.Error(), pkg.ExitManifest)
	}
	archivePath, err := signing.FindArchive(root, name, m.Version)
	if err != nil {
		return nil, pkg.NewErrorWithFix(i18n.T("cat.signature"),
			err.Error(),
			i18n.Tf("sign.error.pack.fix", name),
			pkg.ExitSigning)
	}
	return &signTarget{label: name, archivePath: archivePath, manifest: m}, nil
}

// isArchivePath reports whether arg designates an archive file (by extension
// or by existing file).
func isArchivePath(arg string) bool {
	lower := strings.ToLower(strings.TrimSpace(arg))
	for _, ext := range append([]string{".liozip"}, config.LegacyArchiveExts...) {
		if strings.HasSuffix(lower, strings.ToLower(ext)) {
			return true
		}
	}
	return pkg.FileExists(arg)
}

// signingPayload builds the canonical publication payload (§7.1) for a target:
// archive checksum recomputed from the received bytes plus manifest checksum.
func signingPayload(t *signTarget) (signing.CanonicalPayload, error) {
	checksum, err := signing.ArchiveChecksum(t.archivePath)
	if err != nil {
		return signing.CanonicalPayload{}, err
	}
	manifestChecksum := ""
	if len(t.manifestJSON) > 0 {
		sum := sha256Of(t.manifestJSON)
		manifestChecksum = sum
	} else {
		manifestChecksum = module.ManifestChecksum(t.manifest)
	}
	return signing.CanonicalPayload{
		ModuleIdentifier: t.manifest.Domain,
		Version:          t.manifest.Version,
		Checksum:         checksum,
		ManifestChecksum: manifestChecksum,
		Entry:            t.manifest.Entry,
		Type:             t.manifest.Type,
	}, nil
}

// loadSigningKey returns the private key for signing: `--key` file first,
// keychain otherwise.
func loadSigningKey() (pub ed25519.PublicKey, priv ed25519.PrivateKey, err error) {
	if strings.TrimSpace(signKeyFile) != "" {
		priv, err = signing.LoadPrivateKeyFile(strings.TrimSpace(signKeyFile))
		if err != nil {
			return nil, nil, err
		}
		if l := len(priv); l == ed25519.PrivateKeySize {
			if pubKey, ok := priv.Public().(ed25519.PublicKey); ok {
				return pubKey, priv, nil
			}
		}
		return nil, nil, fmt.Errorf("invalid private key in %s", signKeyFile)
	}
	return signing.LoadKeyPair(signing.NewKeyStore())
}

// signModule signs a module archive with the canonical publication payload
// (spec §7.1, mandatory for publication per ADR-010).
func signModule(args []string) error {
	root, err := requireProjectRoot()
	if err != nil {
		// File mode works outside a project when the path exists.
		if len(args) > 0 && isArchivePath(args[0]) && pkg.FileExists(args[0]) {
			root = ""
		} else {
			return err
		}
	}

	target, err := resolveSignTarget(root, args[0])
	if err != nil {
		return err
	}

	sigPath := target.archivePath + ".sig"
	if pkg.FileExists(sigPath) && tui.IsInteractive() {
		overwrite, err := tui.Confirm(i18n.T("sign.confirm.overwrite"), false)
		if err != nil {
			return err
		}
		if !overwrite {
			return nil
		}
	}

	keys, err := tui.RunWithSpinner(i18n.T("sign.spinner.loading"), func() (*signingKey, error) {
		pub, priv, err := loadSigningKey()
		if err != nil {
			return nil, err
		}
		return &signingKey{pub: pub, priv: priv}, nil
	})
	if err != nil {
		return pkg.NewErrorWithFix(i18n.T("cat.signature"),
			i18n.Tf("sign.error.key_not_found", err.Error()),
			i18n.T("sign.error.keygen.fix"),
			pkg.ExitSigning)
	}

	payload, err := signingPayload(target)
	if err != nil {
		return pkg.NewError(i18n.T("cat.signature"), err.Error(), pkg.ExitSigning)
	}
	payloadBytes := signing.CanonicalPayloadBytes(payload)

	sigPath, err = tui.RunWithSpinner(i18n.T("sign.spinner.signing"), func() (string, error) {
		sig := signing.SignPayload(payloadBytes, keys.priv)
		if err := os.WriteFile(target.archivePath+".sig", sig, 0o600); err != nil {
			return "", fmt.Errorf("failed to write the signature: %w", err)
		}
		return target.archivePath + ".sig", nil
	})
	if err != nil {
		return pkg.NewError(i18n.T("cat.signature"), err.Error(), pkg.ExitSigning)
	}

	// Self-check: the produced signature must verify immediately.
	if !signing.VerifyPayload(payloadBytes, mustRead(sigPath), keys.pub) {
		return pkg.NewError(i18n.T("cat.signature"), i18n.T("publish.error.signature_invalid"), pkg.ExitSigning)
	}

	// Bind the module domain to the key that signed it. When the domain was
	// already bound to another key, several keys are now associated with the
	// module: offer to rotate the previous one (server-side ROTATED status)
	// so only the new key verifies future artefacts.
	fingerprint := signing.Fingerprint(keys.pub)
	_, bindInfo, berr := bindModuleKey(target.manifest.Domain, fingerprint, keys.pub)
	if berr != nil {
		debugf("signing key binding: %v", berr)
	}

	s := tui.NewStyles()
	fmt.Println()
	rows := []string{
		s.KeyValue(i18n.T("label.module"), target.label+" v"+target.manifest.Version),
		s.KeyValue(i18n.T("label.archive"), s.Info.Render(target.archivePath)),
		s.KeyValue(i18n.T("label.signature"), s.Info.Render(sigPath)),
		s.KeyValue(i18n.T("label.signer"), s.Info.Render(fingerprint)),
	}
	if bindInfo != "" {
		rows = append(rows, s.KeyValue(i18n.T("label.binding"), bindInfo))
	}
	fmt.Println(s.SummaryCard(
		s.Success.Render(i18n.T("sign.success")),
		rows...,
	))
	fmt.Println()
	return nil
}

// bindModuleKey records the domain→key association locally and, when the
// domain was bound to a different key, proposes the rotation flow: rotate the
// previous store key (ROTATED), register the new one, refresh the binding.
// It returns the store key id for the new key ("" when unknown), a display
// line for the summary card, and a diagnostic error (never fatal: an offline
// sign must still succeed, the binding is recorded locally).
func bindModuleKey(domain, fingerprint string, pub ed25519.PublicKey) (string, string, error) {
	prev, err := signing.LookupBinding(domain)
	if err != nil {
		return "", "", err
	}

	s := tui.NewStyles()
	if prev == nil || prev.Fingerprint == fingerprint {
		keyID, synced, serr := syncSigningKeyToServer(pub)
		if serr != nil {
			// Binding still recorded locally; the sync is deferred to publish.
			debugf("signing key sync: %v", serr)
		}
		if berr := signing.BindBinding(domain, fingerprint, keyID); berr != nil {
			return keyID, "", berr
		}
		switch {
		case synced:
			return keyID, s.Success.Render(i18n.T("sign.binding.synced")), nil
		default:
			return keyID, s.Warning.Render(i18n.T("sign.binding.local")), nil
		}
	}

	// Another key is already associated with this module domain.
	fmt.Println()
	fmt.Println(s.WarningPanel(s.Warning.Render(i18n.T("sign.binding.other_key")) + "\n\n" +
		s.KeyValue(i18n.T("label.previous_fingerprint"), prev.Fingerprint) + "\n" +
		s.KeyValue(i18n.T("label.new_fingerprint"), fingerprint)))

	rotated := false
	var keyID string
	if tui.IsInteractive() {
		confirm, cerr := tui.Confirm(i18n.T("sign.confirm.rotate"), false)
		if cerr != nil {
			return "", "", cerr
		}
		if confirm {
			if rerr := rotatePreviousKey(prev); rerr != nil {
				debugf("previous key rotation: %v", rerr)
			} else {
				rotated = true
			}
			keyID, _, _ = syncSigningKeyToServer(pub)
		}
	}
	if berr := signing.BindBinding(domain, fingerprint, keyID); berr != nil {
		return keyID, "", berr
	}
	switch {
	case rotated:
		return keyID, s.Success.Render(i18n.T("sign.binding.rotated")), nil
	default:
		return keyID, s.Warning.Render(i18n.T("sign.binding.replaced")), nil
	}
}

// rotatePreviousKey marks the previous store key as ROTATED so the server
// stops accepting artefacts signed with it. Requires an authenticated
// session and a known store key id; offline, it is skipped (the local
// binding is updated regardless).
func rotatePreviousKey(prev *signing.Binding) error {
	if prev == nil || strings.TrimSpace(prev.KeyID) == "" {
		return fmt.Errorf("no store key id recorded for the previous binding")
	}
	sess, err := auth.LoadSession(auth.NewStore())
	if err != nil || sess == nil || !sess.IsAuthenticated() {
		return fmt.Errorf("not connected: the previous key stays ACTIVE until the next publish")
	}
	client := store.NewClient()
	client.SetToken(sess.AccessToken)
	client.WithAutoRefresh(sess)
	if _, err := client.RotateSigningKey(context.Background(), prev.KeyID); err != nil {
		return err
	}
	return nil
}

// verifySignature verifies a module's .sig signature: canonical payload
// first, legacy raw-bytes signature as a migration fallback.
func verifySignature(args []string) error {
	root, err := requireProjectRoot()
	if err != nil {
		if len(args) > 0 && isArchivePath(args[0]) && pkg.FileExists(args[0]) {
			root = ""
		} else {
			return err
		}
	}

	if len(args) > 0 {
		target, terr := resolveSignTarget(root, args[0])
		if terr != nil {
			return terr
		}
		return verifyTarget(target)
	}

	name, err := resolveModule(root, args)
	if err != nil {
		return err
	}
	m, err := module.LoadManifest(config.ManifestPath(root, name))
	if err != nil {
		return pkg.NewError(i18n.T("cat.manifest"), err.Error(), pkg.ExitManifest)
	}
	archivePath, err := signing.FindArchive(root, name, m.Version)
	if err != nil {
		return pkg.NewErrorWithFix(i18n.T("cat.signature"),
			err.Error(),
			i18n.Tf("sign.error.pack.fix", name),
			pkg.ExitSigning)
	}
	return verifyTarget(&signTarget{label: name, archivePath: archivePath, manifest: m})
}

// verifyTarget checks the .sig of one resolved target.
func verifyTarget(target *signTarget) error {
	sigPath := target.archivePath + ".sig"
	if !pkg.FileExists(sigPath) {
		return pkg.NewErrorWithFix(i18n.T("cat.signature"),
			i18n.Tf("sign.error.sig_not_found", sigPath),
			i18n.Tf("sign.error.sign.fix", target.label),
			pkg.ExitSigning)
	}

	type verdict struct {
		valid  bool
		legacy bool
	}
	res, err := tui.RunWithSpinner(i18n.T("sign.spinner.verifying"), func() (*verdict, error) {
		pub, err := verificationKey()
		if err != nil {
			return nil, err
		}
		payload, perr := signingPayload(target)
		if perr != nil {
			return nil, perr
		}
		sig, err := os.ReadFile(sigPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read the signature: %w", err)
		}
		if signing.VerifyPayload(signing.CanonicalPayloadBytes(payload), sig, pub) {
			return &verdict{valid: true}, nil
		}
		// Migration fallback: archives signed before the canonical payload
		// (raw-bytes signature).
		if ok, _ := signing.VerifySignature(target.archivePath, sigPath, pub); ok {
			return &verdict{valid: true, legacy: true}, nil
		}
		return &verdict{}, nil
	})
	if err != nil {
		return pkg.NewErrorWithFix(i18n.T("cat.signature"),
			i18n.Tf("sign.error.verification_key", err.Error()),
			i18n.T("sign.error.keygen.fix"),
			pkg.ExitSigning)
	}

	s := tui.NewStyles()
	fmt.Println()
	if res.valid {
		rows := []string{
			s.KeyValue(i18n.T("label.module"), target.label+" v"+target.manifest.Version),
			s.KeyValue(i18n.T("label.archive"), s.Info.Render(target.archivePath)),
		}
		if res.legacy {
			rows = append(rows, s.KeyValue(i18n.T("label.signature"), s.Warning.Render(i18n.T("sign.valid.legacy"))))
		}
		if pub, err := signing.NewKeyStore().GetPublicKey(); err == nil {
			rows = append(rows,
				s.KeyValue(i18n.T("label.signer"),
					s.Info.Render(signing.Fingerprint(ed25519.PublicKey(pub)))))
		}
		fmt.Println(s.SummaryCard(s.Success.Render(i18n.T("sign.valid")), rows...))
		fmt.Println()
		return nil
	}

	rows := []string{
		s.KeyValue(i18n.T("label.module"), target.label+" v"+target.manifest.Version),
		s.KeyValue(i18n.T("label.archive"), s.Info.Render(target.archivePath)),
	}
	fmt.Println(s.ErrorPanel(
		s.Error.Render(i18n.T("sign.invalid")) + "\n\n" + strings.Join(rows, "\n") + "\n\n" + s.Hint.Render(i18n.T("sign.invalid.hint")),
	))
	return pkg.NewError(i18n.T("cat.signature"), i18n.T("sign.error.invalid"), pkg.ExitSigning)
}

type signingKey struct {
	pub  ed25519.PublicKey
	priv ed25519.PrivateKey
}

// verificationKey returns the public key used to verify a signature: derived
// from `--key` when given, from the keychain otherwise.
func verificationKey() (ed25519.PublicKey, error) {
	if strings.TrimSpace(signKeyFile) != "" {
		pub, _, err := loadSigningKey()
		if err != nil {
			return nil, err
		}
		return pub, nil
	}
	pubBytes, err := signing.NewKeyStore().GetPublicKey()
	if err != nil {
		return nil, err
	}
	return ed25519.PublicKey(pubBytes), nil
}

// sha256Of returns the hex SHA-256 of raw bytes.
func sha256Of(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func mustRead(path string) []byte {
	data, _ := os.ReadFile(path)
	return data
}
