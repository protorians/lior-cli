package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/protorians/lior-cli/internal/config"
	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/module"
	"github.com/protorians/lior-cli/internal/pkg"
	"github.com/protorians/lior-cli/internal/store"
	"github.com/protorians/lior-cli/internal/tui"
	"github.com/spf13/cobra"
)

// flagWebhookVersion narrows `list` to one registry version and overrides the
// declared version of a `push`.
var flagWebhookVersion string

// moduleWebhookCmd groups the webhooks-registry commands of a module (spec
// docs/specs/modules/module-webhooks/features/cli-connect-push.md, chemin 1):
// the declaration `module.webhooks.json` is pushed to the registry via the
// Developer Store.
var moduleWebhookCmd = &cobra.Command{
	Use:   "webhook [module]",
	Short: "Manage a module's webhook registry declarations",
	Long: `Pushes or lists the webhook actions declared in module.webhooks.json of
a module in the central webhooks registry:

  liora module webhook push crm
  liora module webhook list crm --version 1.2.0`,
	Args: cobra.MaximumNArgs(1),
	RunE: runModuleWebhookPush,
}

var moduleWebhookPushCmd = &cobra.Command{
	Use:   "push [module]",
	Short: "Push the module's webhooks declaration to the registry",
	Long: `Loads and validates module.webhooks.json at the root of the module
directory, then registers its actions and subscriptions in the registry
through the Developer Store. A declaration that is missing or invalid is
refused locally — no network call is made.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runModuleWebhookPush,
}

var moduleWebhookListCmd = &cobra.Command{
	Use:   "list [module]",
	Short: "List the module's registered webhooks",
	Long: `Lists the webhook actions of a module held by the registry
(action · version · active). Pass --version to narrow the list to one
declared version.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runModuleWebhookList,
}

func init() {
	moduleWebhookPushCmd.Flags().StringVar(&flagWebhookVersion, "version", "",
		"Version de la déclaration à pousser (défaut : celle déclarée)")
	moduleWebhookListCmd.Flags().StringVar(&flagWebhookVersion, "version", "",
		"Filtrer sur une version déclarée")
}

// runModuleWebhookPush is "path 1" of the webhooks feature (AC-10..AC-12):
// load + validate the declaration locally, resolve the store token, push and
// display the registry summary.
func runModuleWebhookPush(cmd *cobra.Command, args []string) error {
	client, err := connectedClient()
	if err != nil {
		return err
	}
	name, moduleDir, manifest, err := resolveWebhookModule(args)
	if err != nil {
		return err
	}
	declaration, err := module.LoadWebhooksDeclaration(moduleDir)
	if err != nil {
		return pkg.NewError(i18n.T("cat.manifest"), err.Error(), pkg.ExitManifest)
	}
	// G-07: absent declaration — a module without webhooks has nothing to
	// push. The message names the file so the developer knows what to author.
	if declaration == nil {
		return pkg.NewErrorWithFix(i18n.T("cat.manifest"),
			fmt.Sprintf("Aucune déclaration %s pour ce module.", config.WebhooksFileName),
			fmt.Sprintf("Créez %s à la racine du module (actions, subscriptions, version).", config.WebhooksFileName),
			pkg.ExitManifest)
	}
	// An explicit --version pins the pushed declared version.
	if override := strings.TrimSpace(flagWebhookVersion); override != "" {
		declaration.Version = override
	}
	if err := module.ValidateWebhooksDeclaration(declaration, manifest.ID); err != nil {
		return pkg.NewError(i18n.T("cat.manifest"),
			fmt.Sprintf("%s : %v", config.WebhooksFileName, err), pkg.ExitManifest)
	}
	token, err := requireModuleToken(args)
	if err != nil {
		return err
	}
	summary, err := client.PushWebhooks(context.Background(), token, declaration, manifest.ID)
	if err != nil {
		return pkg.NewError(i18n.T("cat.store"), err.Error(), pkg.ExitNetwork)
	}

	s := tui.NewStyles()
	rows := make([][2]string, 0, len(declaration.Actions)+len(declaration.Subscriptions)+1)
	for _, action := range declaration.Actions {
		rows = append(rows, [2]string{action.Action, fmt.Sprintf("version %s", declaration.Version)})
	}
	for _, subscription := range declaration.Subscriptions {
		rows = append(rows, [2]string{subscription.Action, fmt.Sprintf("abonnement · version %s", declaration.Version)})
	}
	rows = append(rows, [2]string{
		"Résumé",
		fmt.Sprintf("%d créées · %d mises à jour · %d désactivées (total %d)",
			summary.Created, summary.Updated, summary.Deactivated, summary.Total),
	})
	fmt.Println()
	fmt.Println(s.Info.Render(fmt.Sprintf("Webhooks du registre : %s v%s", name, declaration.Version)))
	printRows("Webhooks du registre", rows)
	return nil
}

// runModuleWebhookList lists the registry rows of a module in the
// `action · version · actif/inactif` style.
func runModuleWebhookList(cmd *cobra.Command, args []string) error {
	client, err := connectedClient()
	if err != nil {
		return err
	}
	token, err := requireModuleToken(args)
	if err != nil {
		return err
	}
	entries, err := client.ListWebhooks(context.Background(), token, strings.TrimSpace(flagWebhookVersion))
	if err != nil {
		return pkg.NewError(i18n.T("cat.store"), err.Error(), pkg.ExitNetwork)
	}
	rows := make([][2]string, 0, len(entries))
	for _, entry := range entries {
		state := "actif"
		if !entry.IsActive {
			state = "inactif"
		}
		rows = append(rows, [2]string{entry.Action, fmt.Sprintf("%s · %s", entry.Version, state)})
	}
	printRows("Webhooks du registre", rows)
	return nil
}

// resolveWebhookModule resolves the project root, module directory and manifest
// for a webhook sub-command (the same resolution chain `requireModuleToken`
// uses, keeping the manifest that carries the identifier G-02 is checked
// against).
func resolveWebhookModule(args []string) (name, moduleDir string, manifest *module.Manifest, err error) {
	root, err := requireProjectRoot()
	if err != nil {
		return "", "", nil, err
	}
	name, err = resolveModule(root, args)
	if err != nil {
		return "", "", nil, err
	}
	manifest, err = module.LoadManifest(config.ManifestPath(root, name))
	if err != nil {
		return "", "", nil, pkg.NewError(i18n.T("cat.manifest"), err.Error(), pkg.ExitManifest)
	}
	moduleDir = config.ResolveModuleDir(root, name)
	if moduleDir == "" {
		return "", "", nil, pkg.NewError(i18n.T("cat.module"),
			i18n.Tf("val.module_not_found", name, config.WorkspaceModulesDir+"|"+config.ExternalModulesDir),
			pkg.ExitModuleNotFound)
	}
	return name, moduleDir, manifest, nil
}

// pushModuleWebhooksBestEffort relays the webhooks declarations of the current
// project to the registry after a successful connect ("path 2" of the feature,
// spec cli-connect-push §5). It never fails the connect flow: a missing
// project, a missing declaration, a missing token or any network error is a
// quiet warning only.
func pushModuleWebhooksBestEffort() {
	client, err := connectedClient()
	if err != nil {
		return // not connected: the connect flow itself already reports it
	}
	root, err := config.FindProjectRoot("")
	if err != nil {
		return // no project in the cwd: nothing to relay
	}
	names, err := config.ListWorkspaceModules(root)
	if err != nil {
		return
	}
	for _, name := range names {
		pushModuleWebhookForBestEffort(client, root, name)
	}
}

// pushModuleWebhookForBestEffort pushes one module's declaration if it exists;
// every failure is quiet (debug) — best-effort, never blocking.
func pushModuleWebhookForBestEffort(client *store.Client, root, name string) {
	moduleDir := config.WorkspaceModuleDir(root, name)
	declaration, err := module.LoadWebhooksDeclaration(moduleDir)
	if err != nil {
		debugf("webhooks : %s illisible pour %s : %v", config.WebhooksFileName, name, err)
		return
	}
	if declaration == nil {
		return // module without webhooks: unchanged behaviour
	}
	manifest, err := module.LoadManifest(config.ManifestPath(root, name))
	if err != nil {
		debugf("webhooks : manifeste illisible pour %s : %v", name, err)
		return
	}
	if err := module.ValidateWebhooksDeclaration(declaration, manifest.ID); err != nil {
		debugf("webhooks : %s invalide pour %s : %v", config.WebhooksFileName, name, err)
		return
	}
	token := strings.TrimSpace(manifest.Token)
	if token == "" {
		return // unlinked module: `liora publish` / `liora link` owns that path
	}
	if _, err := client.PushWebhooks(context.Background(), token, declaration, manifest.ID); err != nil {
		debugf("webhooks : échec du push du registre pour %s : %v", name, err)
	}
}
