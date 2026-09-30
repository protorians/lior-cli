package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/pkg"
	"github.com/protorians/lior-cli/internal/signing"
	"github.com/spf13/cobra"
)

var (
	signTrustFormat string
	signTrustMerge  string
)

// signTrustCmd exports the developer public key in the form the socle pins it
// (E-007 — `AppConfig.MODULE_TRUST_KEYS`, fed by
// `NEXT_PUBLIC_MODULE_TRUST_KEYS`). Without this export the socle can only
// reach the `unverifiable` verdict: it never sees the key, so a relayed
// artifact is never re-verified locally, and the integrity of the distribution
// channel is only as good as the channel itself.
var signTrustCmd = &cobra.Command{
	Use:   "trust",
	Short: "Export the public key to pin in the socle trust store",
	Long: `Exports the public signing key under the key identifier the store
publishes as 'signatureKeyId' (the SHA-256 fingerprint of the public key).

The socle re-verifies every relayed artifact locally against a trust store
pinned at build time (NEXT_PUBLIC_MODULE_TRUST_KEYS = {"<keyId>": "<PEM>"}).
This command emits that exact material, so the developer side and the usage
side cannot drift:

  liora sign trust                     export NEXT_PUBLIC_MODULE_TRUST_KEYS=…
  liora sign trust --format json       the bare {"<keyId>": "<PEM>"} object
  liora sign trust --format pem        the PEM public key alone
  liora sign trust --format keyid      the key identifier alone (fingerprint)
  liora sign trust --merge keys.json   add the key to an existing trust store

A socle left without a trust store stays usable but never verifies a relayed
artifact: the verdict degrades to 'unverifiable' and the channel is trusted.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSignTrust()
	},
}

func init() {
	signTrustCmd.Flags().StringVar(&signTrustFormat, "format", "env",
		"output format: env, json, pem, keyid")
	signTrustCmd.Flags().StringVar(&signTrustMerge, "merge", "",
		"add the key to an existing trust store file and rewrite it")
	i18nFlag(signTrustCmd, "format", "sign.flag.trust_format")
	i18nFlag(signTrustCmd, "merge", "sign.flag.trust_merge")
	signCmd.AddCommand(signKeygenCmd, signVerifyCmd, signTrustCmd)
	i18nHelp(signTrustCmd, "cmd.sign.trust.short", "cmd.sign.trust.long")
}

// runSignTrust prints (or merges) the pinned public key.
func runSignTrust() error {
	ks := signing.NewKeyStore()
	pub, err := ks.GetPublicKey()
	if err != nil {
		return pkg.NewErrorWithFix(i18n.T("cat.signature"),
			i18n.T("sign.error.no_key"),
			i18n.T("sign.error.keygen.fix"),
			pkg.ExitSigning)
	}
	pem, err := signing.PublicKeyPEM(pub)
	if err != nil {
		return err
	}
	keyID := signing.Fingerprint(pub)

	// A PEM is only usable if the socle accepts it: a block that is not
	// "PUBLIC KEY" is silently dropped by the socle parser
	// (`parseModuleTrustKeys`), which would leave an empty trust store.
	if !strings.Contains(pem, "BEGIN PUBLIC KEY") {
		return pkg.NewError(i18n.T("cat.signature"),
			i18n.Tf("sign.trust.error.pem", "BEGIN PUBLIC KEY"), pkg.ExitSigning)
	}

	ring := map[string]string{}
	if strings.TrimSpace(signTrustMerge) != "" {
		ring, err = loadTrustRing(signTrustMerge)
		if err != nil {
			return err
		}
	}
	ring[keyID] = pem

	value, err := trustRingJSON(ring)
	if err != nil {
		return err
	}

	if path := strings.TrimSpace(signTrustMerge); path != "" {
		if err := os.WriteFile(path, []byte(value+"\n"), 0o600); err != nil {
			return fmt.Errorf("%s: %w", i18n.T("sign.trust.error.merge_write"), err)
		}
	}

	out, err := trustOutput(value, pem, keyID)
	if err != nil {
		return err
	}
	fmt.Print(out)
	return nil
}

// trustOutput renders the export in the requested format. Split from
// `runSignTrust` so the formats are testable without the developer's keychain:
// the four outputs differ in the shell they target, and a mistake here is
// invisible until the socle silently fails to pin the key.
func trustOutput(ringJSON, pem, keyID string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(signTrustFormat)) {
	case "", "env":
		return fmt.Sprintf("export NEXT_PUBLIC_MODULE_TRUST_KEYS='%s'\n", ringJSON), nil
	case "json":
		return ringJSON + "\n", nil
	case "pem":
		return pem, nil
	case "keyid", "fingerprint":
		return keyID + "\n", nil
	default:
		return "", pkg.NewError(i18n.T("cat.signature"),
			i18n.Tf("sign.trust.error.format", signTrustFormat), pkg.ExitError)
	}
}

// loadTrustRing reads an existing trust store: a JSON object of keyId → PEM, or
// a shell `export NEXT_PUBLIC_MODULE_TRUST_KEYS=…` line as found in a `.env`.
func loadTrustRing(path string) (map[string]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("sign.trust.error.merge_read"), err)
	}
	text := strings.TrimSpace(string(raw))
	if _, value, found := strings.Cut(text, "NEXT_PUBLIC_MODULE_TRUST_KEYS="); found {
		text = strings.TrimSpace(value)
		text = strings.Trim(text, "'\"")
	}
	if text == "" {
		return map[string]string{}, nil
	}
	ring := map[string]string{}
	if err := json.Unmarshal([]byte(text), &ring); err != nil {
		return nil, pkg.NewError(i18n.T("cat.signature"),
			i18n.Tf("sign.trust.error.merge_parse", path), pkg.ExitError)
	}
	// Drop anything the socle would ignore, so the rewritten file cannot carry
	// an entry that parses to an empty trust store.
	for key, value := range ring {
		if !strings.Contains(value, "BEGIN PUBLIC KEY") {
			delete(ring, key)
		}
	}
	return ring, nil
}

// trustRingJSON serializes a trust store with deterministic key order: the file
// is committed in the socle, and a stable byte stream keeps diffs readable.
func trustRingJSON(ring map[string]string) (string, error) {
	keys := make([]string, 0, len(ring))
	for key := range ring {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteByte('{')
	for i, key := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		encodedKey, err := json.Marshal(key)
		if err != nil {
			return "", err
		}
		encodedValue, err := json.Marshal(ring[key])
		if err != nil {
			return "", err
		}
		b.Write(encodedKey)
		b.WriteByte(':')
		b.Write(encodedValue)
	}
	b.WriteByte('}')
	return b.String(), nil
}
