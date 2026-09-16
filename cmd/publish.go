package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/protorians/sentient-cli/internal/audit"
	"github.com/protorians/sentient-cli/internal/auth"
	"github.com/protorians/sentient-cli/internal/config"
	"github.com/protorians/sentient-cli/internal/i18n"
	"github.com/protorians/sentient-cli/internal/module"
	"github.com/protorians/sentient-cli/internal/pkg"
	"github.com/protorians/sentient-cli/internal/store"
	"github.com/protorians/sentient-cli/internal/tui"
	"github.com/spf13/cobra"
)

var publishCmd = &cobra.Command{
	Use:   "publish [module]",
	Short: "Publish a module to the store",
	Long: `Builds and publishes a module to the store via the sentient-connect API.

Checks authentication, validates the manifest, builds the .SenMod archive,
then sends it to the store.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPublish(cmd, args)
	},
}

func init() {
	i18nHelp(publishCmd, "cmd.publish.short", "cmd.publish.long")
}

func runPublish(cmd *cobra.Command, args []string) error {
	root, err := requireProjectRoot()
	if err != nil {
		return err
	}

	// Check auth
	sess, err := auth.LoadSession(auth.NewStore())
	if err != nil || sess == nil || !sess.IsAuthenticated() {
		return pkg.NewErrorWithFix(i18n.T("cat.authentication"),
			i18n.T("publish.error.not_connected"),
			i18n.T("publish.error.connect.fix"), pkg.ExitAuth)
	}

	email := ""
	if sess.User != nil {
		email = sess.User.Email
	}

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
	// `"publish".autoAudit` in `sentients.config.json` (default: true).
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

	// Check if metadata is incomplete and prompt
	if tui.IsInteractive() {
		updated := false
		if manifest.Name == "" || manifest.Name == name {
			n, err := tui.AskText(i18n.T("publish.prompt.display_name"), manifest.Name)
			if err != nil {
				return err
			}
			if strings.TrimSpace(n) != "" {
				manifest.Name = strings.TrimSpace(n)
				updated = true
			}
		}
		if manifest.Description == "" {
			d, err := tui.AskText(i18n.T("create.prompt.description"), "")
			if err != nil {
				return err
			}
			if strings.TrimSpace(d) != "" {
				manifest.Description = strings.TrimSpace(d)
				updated = true
			}
		}
		if manifest.Publisher.ID == "" {
			p, err := tui.AskText(i18n.T("publish.prompt.dev_id"), "")
			if err != nil {
				return err
			}
			if strings.TrimSpace(p) != "" {
				manifest.Publisher.ID = strings.TrimSpace(p)
				updated = true
			}
		}
		if manifest.Publisher.Name == "" {
			pn, err := tui.AskText(i18n.T("publish.prompt.dev_name"), "")
			if err != nil {
				return err
			}
			if strings.TrimSpace(pn) != "" {
				manifest.Publisher.Name = strings.TrimSpace(pn)
				updated = true
			}
		}
		if updated {
			if err := manifest.Save(manifestPath); err != nil {
				warn(i18n.Tf("publish.warn.manifest", err.Error()))
			}
		}
	}

	// Display metadata summary
	s := tui.NewStyles()
	fmt.Println()
	fmt.Println(s.NeutralPanel(strings.TrimSpace(
		s.Success.Render(i18n.Tf("publish.success.authenticated", email))+"\n\n"+
			strings.TrimSpace(s.SubHeader.Render(i18n.T("publish.metadata")))+"\n\n"+
			s.KeyValue(i18n.T("label.name"), s.Value.Render(manifest.Name))+"\n"+
			s.KeyValue(i18n.T("label.description"), s.Value.Render(manifest.Description))+"\n"+
			s.KeyValue(i18n.T("label.version"), s.Value.Render(manifest.Version)),
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

	// Pack
	packer := &module.Packer{Root: root}
	packResult, err := tui.RunWithSpinner(i18n.T("pack.spinner"), func() (*module.PackResult, error) {
		return packer.Pack(name)
	})
	if err != nil {
		if _, ok := err.(*pkg.Error); ok {
			return err
		}
		return pkg.NewError(i18n.T("cat.pack"), err.Error(), pkg.ExitBuild)
	}

	client := store.NewClient()
	client.SetToken(sess.AccessToken)

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
				i18n.T("publish.error.fix"), pkg.ExitPublish)
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
	}
	if err != nil {
		return pkg.NewErrorWithFix(i18n.T("cat.publication"), err.Error(),
			i18n.T("publish.error.fix"), pkg.ExitPublish)
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
	if pubResult.URL != "" {
		rows = append(rows, s.KeyValue(i18n.T("label.url"), s.Info.Render(pubResult.URL)))
	}
	fmt.Println(s.SummaryCard(s.Success.Render(i18n.T("publish.success")), rows...))
	fmt.Println()
	return nil
}

// maxPublishAttempts bounds the conflict-retry loop.
const maxPublishAttempts = 5

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
