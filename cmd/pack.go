package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
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

var (
	packOut     string
	packVersion string
	packNoSign  bool
)

var packCmd = &cobra.Command{
	Use:   "pack [<module>[@<version>]]",
	Short: "Build a module archive (.liozip)",
	Long: `Builds a module and creates a compressed .liozip archive
(moved to .lorian/build/).

The archive is signed automatically with the developer signing key
(the key bound to the module domain, or the keychain key) — use
--no-sign to skip. ` + "`liora publish`" + ` re-verifies the signature
locally before the upload and the store verifies it again server-side.

The version can be given with an '@' separator (e.g. com.example.blog-manager@1.2.0),
with --version, or defaults to the manifest version.

Without an argument, a selector lets you choose the module.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPack(cmd, args)
	},
}

func init() {
	packCmd.Flags().StringVar(&packOut, "out", "", i18n.T("pack.flag.out"))
	packCmd.Flags().StringVar(&packVersion, "version", "", i18n.T("pack.flag.version"))
	packCmd.Flags().BoolVar(&packNoSign, "no-sign", false, i18n.T("pack.flag.no_sign"))
	i18nHelp(packCmd, "cmd.pack.short", "cmd.pack.long")
	i18nFlag(packCmd, "out", "pack.flag.out")
	i18nFlag(packCmd, "version", "pack.flag.version")
	i18nFlag(packCmd, "no-sign", "pack.flag.no_sign")
}

func runPack(cmd *cobra.Command, args []string) error {
	root, err := requireProjectRoot()
	if err != nil {
		return err
	}

	name, version, err := resolveModuleWithVersion(root, args)
	if err != nil {
		return err
	}
	if strings.TrimSpace(packVersion) != "" {
		version = strings.TrimSpace(packVersion)
	}

	result, err := tui.RunWithSpinner(i18n.T("pack.spinner"), func() (*module.PackResult, error) {
		p := &module.Packer{Root: root, Version: version, Out: strings.TrimSpace(packOut)}
		return p.Pack(name)
	})
	if err != nil {
		if _, ok := err.(*pkg.Error); ok {
			return err
		}
		return pkg.NewError(i18n.T("cat.pack"), err.Error(), pkg.ExitBuild)
	}

	s := tui.NewStyles()
	fmt.Println()
	rows := []string{
		s.KeyValue(i18n.T("label.module"), name+" v"+result.Version),
		s.KeyValue(i18n.T("label.file"), s.Info.Render(result.Path)),
		s.KeyValue(i18n.T("label.size"), humanSize(result.Size)),
	}
	if result.Checksum != "" {
		rows = append(rows, s.KeyValue(i18n.T("label.checksum"), s.Value.Render(shortDigest(result.Checksum))))
	}
	if result.FileCount > 0 {
		rows = append(rows, s.KeyValue(i18n.T("label.files"), s.Value.Render(fmt.Sprintf("%d", result.FileCount))))
	}

	// Sign the fresh archive with the developer signing key (spec: "liora
	// pack <module.domain> → signe l'artefact (.liozip) du module avec la
	// clé appropriée"). The signature stays optional here: `liora sign` and
	// `liora publish` produce/verify it too.
	if !packNoSign {
		sigInfo, serr := signPackedArchive(root, name, result)
		switch {
		case serr != nil:
			if _, ok := serr.(*pkg.Error); ok {
				return serr
			}
			rows = append(rows, s.KeyValue(i18n.T("label.signature"), s.Warning.Render(i18n.Tf("sign.skipped.reason", serr.Error()))))
		case sigInfo == "":
			rows = append(rows, s.KeyValue(i18n.T("label.signature"), s.Warning.Render(i18n.T("sign.skipped.no_key"))))
		default:
			rows = append(rows, s.KeyValue(i18n.T("label.signature"), s.Success.Render(i18n.T("sign.signed"))+" "+s.Info.Render(sigInfo)))
		}
	}

	fmt.Println(s.SummaryCard(
		s.Success.Render(i18n.T("pack.success")),
		rows...,
	))
	fmt.Println()
	return nil
}

// signPackedArchive signs a freshly packed .liozip with the key bound to the
// module domain (fallback: the keychain key) and verifies the result. It
// returns the signer fingerprint, an empty string when no signing key exists
// (informational skip, not an error), and an error on real failures.
func signPackedArchive(root, name string, result *module.PackResult) (string, error) {
	manifest, err := module.LoadManifest(config.ManifestPath(root, name))
	if err != nil {
		return "", err
	}
	ks := signing.NewKeyStore()
	if !ks.HasKeys() {
		return "", nil
	}
	pub, priv, err := signing.LoadKeyPair(ks)
	if err != nil {
		return "", err
	}
	if binding, berr := signing.LookupBinding(manifest.Domain); berr == nil && binding != nil &&
		binding.Fingerprint != "" && binding.Fingerprint != signing.Fingerprint(pub) {
		warn(i18n.T("sign.bound_key_mismatch"))
	}

	// The server recomputes `mod.<publisherSlug>.<moduleSlug>` to verify the
	// artefact; the identifier must match it. Authenticated, the publisher
	// slug comes from the store account; offline, the server-side default
	// (`developer`) is used as the best-effort guess — `liora publish`
	// re-verifies locally and re-signs when the identifier diverges.
	publisherSlug := ""
	if sess, serr := auth.LoadSession(auth.NewStore()); serr == nil && sess != nil && sess.IsAuthenticated() {
		client := store.NewClient()
		client.SetToken(sess.AccessToken)
		client.WithAutoRefresh(sess)
		if account, aerr := client.GetMyAccount(context.Background()); aerr == nil {
			publisherSlug = account.Slug
		}
	}
	moduleIdentifier := store.CatalogIdentifier(publisherSlug, store.ProductSlug(manifest))

	checksum, err := signing.ArchiveChecksum(result.Path)
	if err != nil {
		return "", err
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
	if err := osWriteFile(result.Path+".sig", sig); err != nil {
		return "", err
	}
	if !signing.VerifyPayload(payload, sig, pub) {
		return "", errors.New(i18n.T("publish.error.signature_invalid"))
	}
	fingerprint := signing.Fingerprint(pub)
	if berr := signing.BindBinding(manifest.Domain, fingerprint, ""); berr != nil {
		debugf("signing key binding: %v", berr)
	}
	return fingerprint, nil
}

// osWriteFile writes the .sig sidecar with user-only permissions.
func osWriteFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o600)
}

// shortDigest renders the leading characters of a hex digest for display.
func shortDigest(hexDigest string) string {
	if len(hexDigest) > 12 {
		return hexDigest[:12] + "…"
	}
	return hexDigest
}

// humanSize renders a byte count in a human-friendly way.
func humanSize(bytes int64) string {
	const kb = 1024
	const mb = kb * 1024
	const gb = mb * 1024
	switch {
	case bytes >= gb:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(gb))
	case bytes >= mb:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(kb))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// normalizeModuleArg strips path decorations from a module argument so both
// `mod.acme.crm`, `./acme-crm` and `library/modules/mod.acme.crm` resolve to
// the module directory name.
func normalizeModuleArg(arg string) string {
	name, _ := splitModuleSpec(arg)
	name = strings.TrimSpace(name)
	name = strings.Trim(name, "/")
	name = strings.TrimPrefix(name, "./")
	name = strings.TrimPrefix(name, "library/modules/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	return name
}
