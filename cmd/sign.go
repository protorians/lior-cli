package cmd

import (
	"crypto/ed25519"
	"fmt"
	"strings"

	"github.com/jetbrains/lior-cli/internal/config"
	"github.com/jetbrains/lior-cli/internal/i18n"
	"github.com/jetbrains/lior-cli/internal/module"
	"github.com/jetbrains/lior-cli/internal/pkg"
	"github.com/jetbrains/lior-cli/internal/signing"
	"github.com/jetbrains/lior-cli/internal/tui"
	"github.com/spf13/cobra"
)

var signCmd = &cobra.Command{
	Use:   "sign [module]",
	Short: "Sign .SenMod archives (Ed25519)",
	Long: `Manages Ed25519 digital signatures for modules: generates keys,
signs the .SenMod archives and verifies signatures.

Subcommands:
  sign keygen            Generate an Ed25519 key pair
  sign <module>          Sign a module's .SenMod archive
  sign verify <module>   Verify a module's signature

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
	Use:   "verify [module]",
	Short: "Verify a module's signature",
	Long: `Verifies the validity of a module's .sig file against its
.SenMod archive, using the public key stored in the keychain.`,
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
	fmt.Println(s.SummaryCard(
		s.Success.Render(i18n.T("sign.keygen.success")),
		s.KeyValue(i18n.T("label.fingerprint"),
			s.Info.Render(signing.Fingerprint(pub))+" (SHA-256 of the public key)"),
		s.KeyValue(i18n.T("label.private_key"), i18n.T("sign.stored_keychain")),
		s.KeyValue(i18n.T("label.public_key"), i18n.T("sign.stored_keychain")),
	))
	fmt.Println()
	return nil
}

// signModule signs a module's .SenMod archive (spec §5.13.2).
func signModule(args []string) error {
	root, err := requireProjectRoot()
	if err != nil {
		return err
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

	sigPath := archivePath + ".sig"
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
		pub, priv, err := signing.LoadKeyPair(signing.NewKeyStore())
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

	sigPath, err = tui.RunWithSpinner(i18n.T("sign.spinner.signing"), func() (string, error) {
		return signing.SignArchive(archivePath, keys.priv)
	})
	if err != nil {
		return pkg.NewError(i18n.T("cat.signature"), err.Error(), pkg.ExitSigning)
	}

	s := tui.NewStyles()
	fmt.Println()
	fmt.Println(s.SummaryCard(
		s.Success.Render(i18n.T("sign.success")),
		s.KeyValue(i18n.T("label.module"), name+" v"+m.Version),
		s.KeyValue(i18n.T("label.archive"), s.Info.Render(archivePath)),
		s.KeyValue(i18n.T("label.signature"), s.Info.Render(sigPath)),
		s.KeyValue(i18n.T("label.signer"), s.Info.Render(signing.Fingerprint(keys.pub))),
	))
	fmt.Println()
	return nil
}

// verifySignature verifies a module's .sig signature (spec §5.13.3).
func verifySignature(args []string) error {
	root, err := requireProjectRoot()
	if err != nil {
		return err
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

	sigPath := archivePath + ".sig"
	if !pkg.FileExists(sigPath) {
		return pkg.NewErrorWithFix(i18n.T("cat.signature"),
			i18n.Tf("sign.error.sig_not_found", sigPath),
			i18n.Tf("sign.error.sign.fix", name),
			pkg.ExitSigning)
	}

	valid, err := tui.RunWithSpinner(i18n.T("sign.spinner.verifying"), func() (bool, error) {
		pubBytes, err := signing.NewKeyStore().GetPublicKey()
		if err != nil {
			return false, err
		}
		return signing.VerifySignature(archivePath, sigPath, ed25519.PublicKey(pubBytes))
	})
	if err != nil {
		return pkg.NewErrorWithFix(i18n.T("cat.signature"),
			i18n.Tf("sign.error.verification_key", err.Error()),
			i18n.T("sign.error.keygen.fix"),
			pkg.ExitSigning)
	}

	s := tui.NewStyles()
	fmt.Println()
	if valid {
		rows := []string{
			s.KeyValue(i18n.T("label.module"), name+" v"+m.Version),
			s.KeyValue(i18n.T("label.archive"), s.Info.Render(archivePath)),
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
		s.KeyValue(i18n.T("label.module"), name+" v"+m.Version),
		s.KeyValue(i18n.T("label.archive"), s.Info.Render(archivePath)),
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
