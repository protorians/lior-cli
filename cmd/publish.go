package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/protorians/lior-cli/internal/audit"
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

var publishCmd = &cobra.Command{
	Use:   "publish [module]",
	Short: "Publish a module to the store",
	Long: `Builds and publishes a module to the store via the liorian-connect API.

Checks authentication, validates the manifest, builds the .liozip archive,
signs the canonical publication payload (mandatory, ADR-010) and sends the
artefact to the store.

Flags --file and --version support the release session flow:
  liora pack ./acme-crm --version 1.3.0 --out acme-crm.liozip
  liora sign acme-crm.liozip --key ~/.acme/ed25519
  liora publish mod.acme.crm --version 1.3.0 --file acme-crm.liozip`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPublish(cmd, args)
	},
}

var (
	publishFile          string
	publishVersion       string
	publishAllowUnsigned bool
)

func init() {
	publishCmd.Flags().StringVar(&publishFile, "file", "", i18n.T("publish.flag.file"))
	publishCmd.Flags().StringVar(&publishVersion, "version", "", i18n.T("publish.flag.version"))
	publishCmd.Flags().BoolVar(&publishAllowUnsigned, "allow-unsigned", false, i18n.T("publish.flag.allow_unsigned"))
	i18nHelp(publishCmd, "cmd.publish.short", "cmd.publish.long")
	i18nFlag(publishCmd, "file", "publish.flag.file")
	i18nFlag(publishCmd, "version", "publish.flag.version")
	i18nFlag(publishCmd, "allow-unsigned", "publish.flag.allow_unsigned")
}

func runPublish(cmd *cobra.Command, args []string) error {
	root, err := requireProjectRoot()
	if err != nil {
		return err
	}

	// Check auth. Spec §5.6 step 1: when not connected, run the `liora
	// connect` flow automatically before publishing.
	s := tui.NewStyles()
	sess, err := auth.LoadSession(auth.NewStore())
	if err != nil || sess == nil || !sess.IsAuthenticated() {
		fmt.Println(s.Info.Render(i18n.T("publish.info.connect")))
		if cerr := doConnect(); cerr != nil {
			return pkg.NewErrorWithFix(i18n.T("cat.authentication"),
				i18n.T("publish.error.not_connected"),
				i18n.T("publish.error.connect.fix"), pkg.ExitAuth)
		}
		sess, err = auth.LoadSession(auth.NewStore())
		if err != nil || sess == nil || !sess.IsAuthenticated() {
			return pkg.NewErrorWithFix(i18n.T("cat.authentication"),
				i18n.T("publish.error.not_connected"),
				i18n.T("publish.error.connect.fix"), pkg.ExitAuth)
		}
	}

	email := ""
	if sess.User != nil {
		email = sess.User.Email
	}

	// The Developer Store client is built once: the developer identity is
	// resolved from it before the metadata prompts, then reused to publish.
	client := store.NewClient()
	client.SetToken(sess.AccessToken)
	client.WithAutoRefresh(sess)

	// Identify module
	name, err := resolveModule(root, args)
	if err != nil {
		return err
	}

	// Load and validate manifest
	manifestPath := config.ManifestPath(root, name)
	manifest, err := module.LoadManifest(manifestPath)
	if err != nil {
		return pkg.NewError(i18n.T("cat.manifest"), err.Error(), pkg.ExitManifest)
	}

	// Auto-audit before publishing (spec §5.6) — configurable via
	// `"publish".autoAudit` in `lorian.config.json` (default: true).
	cfg, err := config.Load(config.ConfigPath(root))
	if err != nil {
		debugf("reading configuration: %v", err)
	}
	if cfg.Publish.AutoAudit {
		auditRes, aerr := (&audit.Auditor{Root: root}).AuditModules(name)
		if aerr != nil {
			return pkg.NewError(i18n.T("cat.audit"), aerr.Error(), pkg.ExitError)
		}
		if errs := auditRes.TotalErrors(); errs > 0 {
			printAuditResult(auditRes)
			if !tui.IsInteractive() {
				return pkg.NewErrorWithFix(i18n.T("cat.audit"),
					i18n.Tf("publish.error.audit", name, errs),
					i18n.Tf("publish.error.audit.fix", name),
					pkg.ExitError)
			}
			continueAnyway, cerr := tui.Confirm(i18n.T("publish.confirm.despit"), false)
			if cerr != nil {
				return cerr
			}
			if !continueAnyway {
				return nil
			}
			warn(i18n.T("publish.warn.despit"))
		}
	}

	// Complete the manifest metadata. The developer identity is resolved from the
	// store, never typed by hand; only the remaining blanks are prompted for.
	manifestUpdated := resolveDeveloperIdentity(context.Background(), client, sess, manifest)

	if tui.IsInteractive() {
		if manifest.Name == "" || manifest.Name == name {
			n, err := tui.AskText(i18n.T("publish.prompt.display_name"), manifest.Name)
			if err != nil {
				return err
			}
			if strings.TrimSpace(n) != "" {
				manifest.Name = strings.TrimSpace(n)
				manifestUpdated = true
			}
		}
		if manifest.Description == "" {
			d, err := tui.AskText(i18n.T("create.prompt.description"), "")
			if err != nil {
				return err
			}
			if strings.TrimSpace(d) != "" {
				manifest.Description = strings.TrimSpace(d)
				manifestUpdated = true
			}
		}
		if manifest.Publisher.Name == "" {
			pn, err := tui.AskText(i18n.T("publish.prompt.dev_name"), "")
			if err != nil {
				return err
			}
			if strings.TrimSpace(pn) != "" {
				manifest.Publisher.Name = strings.TrimSpace(pn)
				manifestUpdated = true
			}
		}
	}
	if manifestUpdated {
		if err := manifest.Save(manifestPath); err != nil {
			warn(i18n.Tf("publish.warn.manifest", err.Error()))
		}
	}

	// Display metadata summary
	fmt.Println()
	fmt.Println(s.NeutralPanel(strings.TrimSpace(
		s.Success.Render(i18n.Tf("publish.success.authenticated", email)) + "\n\n" +
			strings.TrimSpace(s.SubHeader.Render(i18n.T("publish.metadata"))) + "\n\n" +
			s.KeyValue(i18n.T("label.name"), s.Value.Render(manifest.Name)) + "\n" +
			s.KeyValue(i18n.T("label.description"), s.Value.Render(manifest.Description)) + "\n" +
			s.KeyValue(i18n.T("label.version"), s.Value.Render(effectivePublishVersion(manifest))) + "\n" +
			s.KeyValue(i18n.T("label.type"), s.Value.Render(manifest.Type)) + "\n" +
			s.KeyValue(i18n.T("label.publisher"), s.Value.Render(publisherLabel(manifest))),
	)))

	// Confirm
	if tui.IsInteractive() {
		confirm, err := tui.Confirm(i18n.T("publish.confirm"), true)
		if err != nil {
			return err
		}
		if !confirm {
			return nil
		}
	}

	// An explicit --version pins the published version for this run.
	if strings.TrimSpace(publishVersion) != "" {
		manifest.Version = strings.TrimSpace(publishVersion)
		if err := manifest.Save(manifestPath); err != nil {
			return pkg.NewError(i18n.T("cat.manifest"), err.Error(), pkg.ExitManifest)
		}
	}

	// Pack (or reuse --file, e.g. a pre-built release archive).
	packer := &module.Packer{Root: root}
	var packResult *module.PackResult
	if strings.TrimSpace(publishFile) != "" {
		archivePath := strings.TrimSpace(publishFile)
		info, err := os.Stat(archivePath)
		if err != nil {
			return pkg.NewErrorWithFix(i18n.T("cat.pack"),
				i18n.Tf("publish.error.file_not_found", archivePath),
				i18n.T("publish.error.file_not_found.fix"), pkg.ExitBuild)
		}
		if info.Size() > module.MaxArchiveSize {
			return pkg.NewError(i18n.T("cat.pack"),
				i18n.Tf("pack.error.max_size", module.MaxArchiveSize/(1024*1024)), pkg.ExitBuild)
		}
		packResult = &module.PackResult{
			Module:  name,
			Version: manifest.Version,
			Path:    archivePath,
			Size:    info.Size(),
		}
	} else {
		var err error
		packResult, err = tui.RunWithSpinner(i18n.T("pack.spinner"), func() (*module.PackResult, error) {
			return packer.Pack(name)
		})
		if err != nil {
			if _, ok := err.(*pkg.Error); ok {
				return err
			}
			return pkg.NewError(i18n.T("cat.pack"), err.Error(), pkg.ExitBuild)
		}
	}

	// Sign the canonical publication payload before publishing (spec §7.1 /
	// ADR-010): the signature is mandatory — the server refuses unsigned
	// artefacts. `--allow-unsigned` keeps an explicit escape hatch (dev/mock).
	// The module identifier is the one the server recomputes from the product
	// (`mod.<publisherSlug>.<moduleSlug>`) — signing `manifest.Domain` would
	// diverge whenever the publisher slug is not the one recorded locally.
	accountSlug := ""
	if account, aerr := client.GetMyAccount(context.Background()); aerr == nil {
		accountSlug = account.Slug
	} else {
		debugf("developer account slug lookup: %v", aerr)
	}
	moduleIdentifier := store.CatalogIdentifier(accountSlug, store.ProductSlug(manifest))

	// Local verification/signing BEFORE the upload (spec: verify the
	// signature locally first — an existing .sig is checked against the
	// canonical payload; an unsigned artefact prompts the developer to sign
	// first; a stale .sig is re-signed). The store then verifies the
	// signature again on reception.
	signed, keyID, publicKeyPEM, serr := signOrReuseForPublish(packResult.Path, manifest, moduleIdentifier)
	if serr != nil {
		return pkg.NewError(i18n.T("cat.signature"), serr.Error(), pkg.ExitSigning)
	}
	if !signed {
		warn(i18n.T("publish.warn.unsigned"))
	}

	// Register the local public key on the store before the upload: the server
	// verifies the signature against an ACTIVE `DeveloperSigningKey.publicKey`
	// and rejects the artefact (422) when the account carries none. Idempotent
	// — an already-registered identical key is reused.
	if signed {
		key, created, kerr := client.EnsureSigningKey(context.Background(), publicKeyPEM)
		if kerr != nil {
			return pkg.NewErrorWithFix(i18n.T("cat.signature"), kerr.Error(),
				i18n.T("publish.error.key_register.fix"), pkg.ExitSigning)
		}
		if created {
			fmt.Println(s.Info.Render(i18n.T("publish.info.key_registered")))
		}
		if key != nil && strings.TrimSpace(keyID) == "" {
			keyID = key.KeyID
		}
	}

	// Publish, resolving SemVer conflicts by offering a patch bump on retry
	// (spec §5.6 step 6).
	var pubResult *store.PublishResponse
	for attempt := 0; attempt < maxPublishAttempts; attempt++ {
		pubResult, err = tui.RunWithSpinner(i18n.T("publish.spinner.publish"), func() (*store.PublishResponse, error) {
			return client.Publish(context.Background(), packResult.Path, manifest)
		})
		if err == nil {
			break
		}
		if !isVersionConflict(err) {
			return pkg.NewErrorWithFix(i18n.T("cat.publication"), err.Error(),
				publishErrorFix(err), pkg.ExitPublish)
		}
		if !tui.IsInteractive() {
			return pkg.NewErrorWithFix(i18n.T("cat.publication"), err.Error(),
				i18n.Tf("publish.error.bump.fix", name),
				pkg.ExitPublish)
		}

		warn(i18n.Tf("publish.warn.version_conflict", err.Error()))
		bump, cerr := tui.Confirm(i18n.T("publish.confirm.bump"), true)
		if cerr != nil {
			return cerr
		}
		if !bump {
			return nil
		}
		newVersion, verr := pkg.BumpPatch(manifest.Version)
		if verr != nil {
			return pkg.NewError(i18n.T("cat.manifest"), verr.Error(), pkg.ExitManifest)
		}
		manifest.Version = newVersion
		if err := manifest.Save(manifestPath); err != nil {
			return pkg.NewError(i18n.T("cat.manifest"), err.Error(), pkg.ExitManifest)
		}
		fmt.Println(s.KeyValue(i18n.T("label.new_version"), s.Info.Render(newVersion)))

		packResult, err = tui.RunWithSpinner(i18n.T("publish.spinner.rebuild"), func() (*module.PackResult, error) {
			return packer.Pack(name)
		})
		if err != nil {
			return pkg.NewError(i18n.T("cat.pack"), err.Error(), pkg.ExitBuild)
		}
		if resigned, newKeyID, _, serr := signForPublish(packResult.Path, manifest, moduleIdentifier); serr != nil {
			return pkg.NewError(i18n.T("cat.signature"), serr.Error(), pkg.ExitSigning)
		} else {
			signed = resigned
			keyID = newKeyID
			if !signed {
				warn(i18n.T("publish.warn.unsigned"))
			}
		}
	}
	if err != nil {
		return pkg.NewErrorWithFix(i18n.T("cat.publication"), err.Error(),
			publishErrorFix(err), pkg.ExitPublish)
	}

	// Sync the local manifest with the published artifact (spec §5.6 step 7):
	// the resolved remote product id (token) and the published version.
	updated := false
	if pubResult.Token != "" && pubResult.Token != manifest.Token {
		manifest.Token = pubResult.Token
		updated = true
	}
	if pubResult.Version != "" && pubResult.Version != manifest.Version {
		manifest.Version = pubResult.Version
		updated = true
	}
	if updated {
		if err := manifest.Save(manifestPath); err != nil {
			warn(i18n.Tf("publish.warn.manifest", err.Error()))
		}
	}

	// Best-effort remote metadata sync via PUT /api/developer-store/modules/:id.
	if pubResult.Token != "" {
		if err := client.UpdateModule(context.Background(), pubResult.Token, manifest); err != nil {
			debugf("remote metadata sync: %v", err)
		}
	}

	fmt.Println()
	rows := []string{
		s.KeyValue(i18n.T("label.module"), s.Value.Render(name+" v"+pubResult.Version)),
	}
	if keyID != "" {
		rows = append(rows, s.KeyValue(i18n.T("label.signature_key"), s.Value.Render(shortDigest(keyID))))
	}
	if pubResult.URL != "" {
		rows = append(rows, s.KeyValue(i18n.T("label.url"), s.Info.Render(pubResult.URL)))
	}
	fmt.Println(s.SummaryCard(s.Success.Render(i18n.T("publish.success")), rows...))
	fmt.Println()
	return nil
}

// maxPublishAttempts bounds the conflict-retry loop.
const maxPublishAttempts = 5

// resolveDeveloperIdentity fills `manifest.publisher` from the developer's own
// store account (`GET /api/developer-store/accounts/me`), falling back to the
// session user when that endpoint is unreachable.
//
// The identifier is the Liorian account id `liorian-connect` scopes every
// resource to (`DeveloperProduct.developerId`, extracted from the session JWT
// by `DeveloperAuthMiddleware`) — it is never an Apple or third-party
// developer id, and the server is the one that assigns it. It is therefore
// resolved rather than asked for: the store never receives a `publisher` field,
// so the block is local display metadata kept in sync with the remote account.
//
// It reports whether the manifest was modified.
func resolveDeveloperIdentity(ctx context.Context, c *store.Client, sess *auth.Session, m *module.Manifest) bool {
	if m == nil {
		return false
	}
	// Nothing to resolve — avoid the round trip on an already-synced manifest.
	if strings.TrimSpace(m.Publisher.ID) != "" && strings.TrimSpace(m.Publisher.Name) != "" {
		return false
	}

	updated := false
	account, err := c.GetMyAccount(ctx)
	if err != nil {
		debugf("developer account lookup: %v", err)
	} else {
		if strings.TrimSpace(m.Publisher.ID) == "" {
			if id := strings.TrimSpace(account.ID); id != "" {
				m.Publisher.ID = id
				updated = true
			}
		}
		if strings.TrimSpace(m.Publisher.Name) == "" {
			if name := strings.TrimSpace(account.Name); name != "" {
				m.Publisher.Name = name
				updated = true
			}
		}
	}

	// Offline fallback: the session user id, restored from the keychain.
	if strings.TrimSpace(m.Publisher.ID) == "" && sess != nil && sess.User != nil {
		if id := strings.TrimSpace(sess.User.ID); id != "" {
			m.Publisher.ID = id
			updated = true
		}
	}
	return updated
}

// publisherLabel renders the resolved publisher for the metadata summary
// ("name (id)", or the id alone when the display name is still unknown).
func publisherLabel(m *module.Manifest) string {
	if m == nil {
		return ""
	}
	name := strings.TrimSpace(m.Publisher.Name)
	id := strings.TrimSpace(m.Publisher.ID)
	switch {
	case name != "" && id != "":
		return name + " (" + id + ")"
	case name != "":
		return name
	default:
		return id
	}
}

// signOrReuseForPublish verifies the local signature of the artefact before
// the upload. An existing valid .sig is reused as-is; a stale one (checksum,
// manifest or identifier drift) triggers a re-sign; a missing one prompts the
// developer to sign first when interactive. `--allow-unsigned` keeps the
// explicit unsigned escape hatch (dev/mock only — the server refuses it).
func signOrReuseForPublish(archivePath string, manifest *module.Manifest, moduleIdentifier string) (bool, string, string, error) {
	sigPath := archivePath + ".sig"
	if pkg.FileExists(sigPath) {
		valid, verr := verifyLocalSignature(archivePath, manifest, moduleIdentifier)
		if verr != nil {
			return false, "", "", verr
		}
		if valid {
			pub, _, lerr := signing.LoadKeyPair(signing.NewKeyStore())
			if lerr == nil {
				if pemKey, perr := signing.PublicKeyPEM(pub); perr == nil {
					debugf("reusing the existing local signature (%s)", sigPath)
					return true, signing.Fingerprint(pub), pemKey, nil
				}
			}
		} else {
			warn(i18n.T("publish.warn.local_signature_stale"))
		}
	} else if tui.IsInteractive() && !publishAllowUnsigned && signing.NewKeyStore().HasKeys() {
		answer, aerr := tui.Confirm(i18n.T("publish.prompt.sign_now"), true)
		if aerr != nil {
			return false, "", "", aerr
		}
		if !answer {
			return false, "", "", errors.New(i18n.T("publish.error.signature_required"))
		}
	}
	return signForPublish(archivePath, manifest, moduleIdentifier)
}

// verifyLocalSignature checks the artefact's .sig against the canonical
// publication payload (same fields the store re-verifies on reception).
func verifyLocalSignature(archivePath string, manifest *module.Manifest, moduleIdentifier string) (bool, error) {
	pub, _, err := signing.LoadKeyPair(signing.NewKeyStore())
	if err != nil {
		return false, err
	}
	checksum, err := signing.ArchiveChecksum(archivePath)
	if err != nil {
		return false, err
	}
	sig, err := os.ReadFile(archivePath + ".sig")
	if err != nil {
		return false, fmt.Errorf("failed to read the signature: %w", err)
	}
	payload := signing.CanonicalPayloadBytes(signing.CanonicalPayload{
		ModuleIdentifier: moduleIdentifier,
		Version:          manifest.Version,
		Checksum:         checksum,
		ManifestChecksum: module.ManifestChecksum(manifest),
		Entry:            manifest.Entry,
		Type:             manifest.Type,
	})
	return signing.VerifyPayload(payload, sig, pub), nil
}

// signForPublish signs the canonical publication payload of an archive
// (spec §7.1) and verifies the produced signature. The signature is mandatory
// (ADR-010): without a signing key it returns a descriptive error unless
// `--allow-unsigned` was passed, in which case it returns signed=false and the
// publish proceeds explicitly unsigned (dev/mock only). It also returns the
// signature key id (public-key fingerprint) and the PEM/SPKI public key for
// the store registration (`EnsureSigningKey`).
func signForPublish(archivePath string, manifest *module.Manifest, moduleIdentifier string) (signed bool, keyID string, publicKeyPEM string, err error) {
	ks := signing.NewKeyStore()
	if !ks.HasKeys() {
		if publishAllowUnsigned {
			return false, "", "", nil
		}
		return false, "", "", errors.New(i18n.T("publish.error.signature_required"))
	}

	pub, priv, err := signing.LoadKeyPair(ks)
	if err != nil {
		return false, "", "", err
	}

	checksum, err := signing.ArchiveChecksum(archivePath)
	if err != nil {
		return false, "", "", err
	}
	payload := signing.CanonicalPayloadBytes(signing.CanonicalPayload{
		ModuleIdentifier: moduleIdentifier,
		Version:          manifest.Version,
		Checksum:         checksum,
		ManifestChecksum: module.ManifestChecksum(manifest),
		Entry:            manifest.Entry,
		Type:             manifest.Type,
	})

	sig := signing.SignPayload(payload, priv)
	sigPath := archivePath + ".sig"
	if err := os.WriteFile(sigPath, sig, 0o600); err != nil {
		return false, "", "", fmt.Errorf("failed to write the signature: %w", err)
	}

	if !signing.VerifyPayload(payload, sig, pub) {
		return false, "", "", errors.New(i18n.T("publish.error.signature_invalid"))
	}
	pemKey, err := signing.PublicKeyPEM(pub)
	if err != nil {
		return false, "", "", err
	}
	return true, signing.Fingerprint(pub), pemKey, nil
}

// effectivePublishVersion returns the version that will be published (the
// explicit --version flag wins over the manifest version).
func effectivePublishVersion(manifest *module.Manifest) string {
	if strings.TrimSpace(publishVersion) != "" {
		return strings.TrimSpace(publishVersion)
	}
	return manifest.Version
}

// isVersionConflict reports whether a publish error is a version conflict
// (HTTP 409 or an identifiable message).
func isVersionConflict(err error) bool {
	var apiErr *pkg.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	if apiErr.StatusCode == http.StatusConflict {
		return true
	}
	lower := strings.ToLower(apiErr.Message)
	return strings.Contains(lower, "version") &&
		(strings.Contains(lower, "exists") || strings.Contains(lower, "already") ||
			strings.Contains(lower, "conflict"))
}

// publishErrorFix returns the actionable hint for a failed publication. A
// missing developer-store route is a base-URL misconfiguration, not a
// connectivity problem, so it gets its own hint instead of the generic one;
// the same applies to signature rejections (HTTP 422): the archive was
// received, only its authenticity could not be established.
func publishErrorFix(err error) string {
	if errors.Is(err, store.ErrNoStoreRoute) {
		return i18n.Tf("publish.error.no_route.fix", store.EnvConnectAPI)
	}
	var apiErr *pkg.APIError
	if errors.As(err, &apiErr) {
		message := strings.ToLower(apiErr.Message)
		if strings.Contains(message, "signature") {
			if strings.Contains(message, "clé") || strings.Contains(message, "key") {
				return i18n.T("publish.error.signature_key.fix")
			}
			return i18n.T("publish.error.signature_mismatch.fix")
		}
	}
	return i18n.T("publish.error.fix")
}
