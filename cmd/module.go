package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/protorians/lior-cli/internal/auth"
	"github.com/protorians/lior-cli/internal/config"
	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/module"
	"github.com/protorians/lior-cli/internal/pkg"
	"github.com/protorians/lior-cli/internal/store"
	"github.com/protorians/lior-cli/internal/tui"
	"github.com/spf13/cobra"
)

// moduleCmd groups the Developer Store "module lifecycle" commands (Expo-like
// sections served by liorian-api-connect): knowledge, workflows, channels,
// platforms, requirements, signing keys, accreditations, environment variables,
// GitHub and observability.
var moduleCmd = &cobra.Command{
	Use:   "module",
	Short: "Inspect a module's lifecycle on Liorian Connect",
	Long: `Inspects the Developer Store resources of a published module:
documentation, workflows, build channels, platforms, requirements, signing keys,
accreditations, environment variables, GitHub and observability.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	moduleCmd.AddCommand(
		moduleListCmd,
		moduleKnowledgeCmd,
		moduleWorkflowsCmd,
		moduleChannelsCmd,
		modulePlatformsCmd,
		moduleRequirementsCmd,
		moduleSigningKeysCmd,
		moduleAccreditationsCmd,
		moduleVariablesCmd,
		moduleGithubCmd,
		moduleObserverCmd,
		moduleUsageCmd,
	)
}

// --- list ------------------------------------------------------------------

var moduleListCmd = &cobra.Command{
	Use:   "list",
	Short: "List the modules registered on the Developer Store",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := connectedClient()
		if err != nil {
			return err
		}
		products, err := client.ListModules(context.Background())
		if err != nil {
			return pkg.NewError(i18n.T("cat.store"), err.Error(), pkg.ExitNetwork)
		}
		rows := make([][2]string, 0, len(products))
		for _, m := range products {
			version := m.Version
			if version == "" {
				version = "—"
			}
			rows = append(rows, [2]string{m.Name, fmt.Sprintf("%s · %s", version, m.Status)})
		}
		printRows("Modules", rows)
		return nil
	},
}

// connectedClient returns an authenticated Developer Store client or a
// dedicated authentication error.
func connectedClient() (*store.Client, error) {
	sess, err := auth.LoadSession(auth.NewStore())
	if err != nil || sess == nil || !sess.IsAuthenticated() {
		return nil, pkg.NewErrorWithFix(i18n.T("cat.authentication"),
			i18n.T("publish.error.not_connected"),
			i18n.T("publish.error.connect.fix"), pkg.ExitAuth)
	}
	client := store.NewClient()
	client.SetToken(sess.AccessToken)
	client.WithAutoRefresh(sess)
	return client, nil
}

// requireModuleToken resolves the local module and returns its remote product
// token (the store identifier used by the product-scoped endpoints).
func requireModuleToken(args []string) (string, error) {
	root, err := requireProjectRoot()
	if err != nil {
		return "", err
	}
	name, err := resolveModule(root, args)
	if err != nil {
		return "", err
	}
	manifest, err := module.LoadManifest(config.ManifestPath(root, name))
	if err != nil {
		return "", pkg.NewError(i18n.T("cat.manifest"), err.Error(), pkg.ExitManifest)
	}
	token := strings.TrimSpace(manifest.Token)
	if token == "" {
		return "", pkg.NewErrorWithFix(i18n.T("cat.module"),
			fmt.Sprintf("Le module %q n'est pas lié au store (token absent).", name),
			fmt.Sprintf("Exécutez « liorian publish %s » ou « liorian link %s <token> ».", name, name),
			pkg.ExitModuleNotFound)
	}
	return token, nil
}

// printRows renders a titled table of `label: value` rows.
func printRows(title string, rows [][2]string) {
	s := tui.NewStyles()
	fmt.Println()
	fmt.Println(s.SubHeader.Render(title))
	if len(rows) == 0 {
		fmt.Println(s.Muted.Render("  (aucun élément)"))
		fmt.Println()
		return
	}
	var lines []string
	for _, row := range rows {
		lines = append(lines, s.KeyValue(row[0], s.Value.Render(row[1])))
	}
	fmt.Println(strings.Join(lines, "\n"))
	fmt.Println()
}

// --- knowledge -------------------------------------------------------------

var (
	flagKnowledgeKind      string
	flagKnowledgeSummary   string
	flagKnowledgeReading   int
	flagKnowledgePublish   bool
	flagKnowledgeAsDraft   bool
	flagKnowledgeArticleID string
)

var moduleKnowledgeCmd = &cobra.Command{
	Use:   "knowledge [module]",
	Short: "Manage a module's documentation articles",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runModuleKnowledgeList,
}

var moduleKnowledgeListCmd = &cobra.Command{
	Use:   "list [module]",
	Short: "List a module's documentation articles",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runModuleKnowledgeList,
}

var moduleKnowledgeAddCmd = &cobra.Command{
	Use:   "add <title>",
	Short: "Create a documentation article",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		token, err := requireModuleToken(nil)
		if err != nil {
			return err
		}
		client, err := connectedClient()
		if err != nil {
			return err
		}
		kind := strings.ToUpper(strings.TrimSpace(flagKnowledgeKind))
		if kind == "" {
			kind = "GUIDE"
		}
		body := store.CreateKnowledgeArticleRequest{
			Title:          strings.TrimSpace(args[0]),
			Slug:           slugify(args[0]),
			Kind:           kind,
			Summary:        strings.TrimSpace(flagKnowledgeSummary),
			ReadingMinutes: flagKnowledgeReading,
			Publish:        flagKnowledgePublish && !flagKnowledgeAsDraft,
		}
		article, err := client.CreateKnowledgeArticle(context.Background(), token, body)
		if err != nil {
			return pkg.NewError(i18n.T("cat.store"), err.Error(), pkg.ExitNetwork)
		}
		printRows("Connaissances", [][2]string{
			{article.Title, fmt.Sprintf("%s · %s", article.Slug, article.Status)},
		})
		return nil
	},
}

var moduleKnowledgePublishCmd = &cobra.Command{
	Use:   "publish [module]",
	Short: "Publish a documentation article",
	Long: `Publishes a documentation article. With --draft the article is flipped back
to the draft state. When no article id is supplied, an interactive selection is
offered (non-interactive shells require --id).`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		token, err := requireModuleToken(args)
		if err != nil {
			return err
		}
		client, err := connectedClient()
		if err != nil {
			return err
		}
		articleID := strings.TrimSpace(flagKnowledgeArticleID)
		if articleID == "" {
			articles, err := client.ListKnowledgeArticles(context.Background(), token)
			if err != nil {
				return pkg.NewError(i18n.T("cat.store"), err.Error(), pkg.ExitNetwork)
			}
			articleID, err = selectKnowledgeArticle(articles)
			if err != nil {
				return err
			}
		}
		article, err := client.PublishKnowledgeArticle(context.Background(), token, articleID, !flagKnowledgeAsDraft)
		if err != nil {
			return pkg.NewError(i18n.T("cat.store"), err.Error(), pkg.ExitNetwork)
		}
		printRows("Connaissances", [][2]string{{article.Title, article.Status}})
		return nil
	},
}

func runModuleKnowledgeList(cmd *cobra.Command, args []string) error {
	token, err := requireModuleToken(args)
	if err != nil {
		return err
	}
	client, err := connectedClient()
	if err != nil {
		return err
	}
	articles, err := client.ListKnowledgeArticles(context.Background(), token)
	if err != nil {
		return pkg.NewError(i18n.T("cat.store"), err.Error(), pkg.ExitNetwork)
	}
	rows := make([][2]string, 0, len(articles))
	for _, a := range articles {
		rows = append(rows, [2]string{a.Title, fmt.Sprintf("%s · %s · %d min", a.Kind, a.Status, a.ReadingMinutes)})
	}
	printRows("Connaissances", rows)
	return nil
}

func selectKnowledgeArticle(articles []store.KnowledgeArticle) (string, error) {
	if len(articles) == 0 {
		return "", pkg.NewError(i18n.T("cat.selection"), i18n.T("module.error.no_articles"), pkg.ExitError)
	}
	if len(articles) == 1 {
		return articles[0].ID, nil
	}
	if !tui.IsInteractive() {
		return "", pkg.NewError(i18n.T("cat.selection"), i18n.T("module.error.select_article"), pkg.ExitError)
	}
	labels := make([]string, 0, len(articles))
	index := make(map[string]string, len(articles))
	for _, a := range articles {
		label := fmt.Sprintf("%s (%s)", a.Title, a.Status)
		labels = append(labels, label)
		index[label] = a.ID
	}
	selected, err := tui.Select(i18n.T("module.prompt.select_article"), labels)
	if err != nil {
		return "", err
	}
	return index[selected], nil
}

// --- workflows -------------------------------------------------------------

var (
	flagWorkflowRunID string
	flagWorkflowForce bool
)

var moduleWorkflowsCmd = &cobra.Command{
	Use:   "workflow [module]",
	Short: "Manage a module's CI/CD workflows",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runModuleWorkflowList,
}

var moduleWorkflowListCmd = &cobra.Command{
	Use:   "list [module]",
	Short: "List a module's CI/CD workflows",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runModuleWorkflowList,
}

var moduleWorkflowRunCmd = &cobra.Command{
	Use:   "run [module]",
	Short: "Trigger a workflow execution",
	Long: `Triggers the execution of a workflow. When no workflow id is supplied,
an interactive selection is offered (use --id in non-interactive shells).`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		token, err := requireModuleToken(args)
		if err != nil {
			return err
		}
		client, err := connectedClient()
		if err != nil {
			return err
		}
		workflowID := strings.TrimSpace(flagWorkflowRunID)
		if workflowID == "" {
			workflows, err := client.ListWorkflows(context.Background(), token)
			if err != nil {
				return pkg.NewError(i18n.T("cat.store"), err.Error(), pkg.ExitNetwork)
			}
			workflowID, err = selectWorkflow(workflows)
			if err != nil {
				return err
			}
		}
		workflow, err := client.RunWorkflow(context.Background(), token, workflowID)
		if err != nil {
			return pkg.NewError(i18n.T("cat.store"), err.Error(), pkg.ExitNetwork)
		}
		printRows("Workflows", [][2]string{
			{workflow.Name, fmt.Sprintf("%s · %d exécutions", workflow.Status, workflow.RunCount)},
		})
		return nil
	},
}

var moduleWorkflowDeleteCmd = &cobra.Command{
	Use:   "delete [module]",
	Short: "Delete a workflow declaration",
	Long: `Deletes a workflow declaration. When no workflow id is supplied, an
interactive selection is offered (use --id in non-interactive shells).`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		token, err := requireModuleToken(args)
		if err != nil {
			return err
		}
		client, err := connectedClient()
		if err != nil {
			return err
		}
		workflowID := strings.TrimSpace(flagWorkflowRunID)
		name := workflowID
		if workflowID == "" {
			workflows, err := client.ListWorkflows(context.Background(), token)
			if err != nil {
				return pkg.NewError(i18n.T("cat.store"), err.Error(), pkg.ExitNetwork)
			}
			selected, err := selectWorkflow(workflows)
			if err != nil {
				return err
			}
			workflowID = selected
			name = workflowLabel(workflows, selected)
		}
		if !flagWorkflowForce {
			ok, err := tui.Confirm(fmt.Sprintf(i18n.T("module.prompt.confirm_delete"), name), false)
			if err != nil {
				return err
			}
			if !ok {
				return nil
			}
		}
		if err := client.DeleteWorkflow(context.Background(), token, workflowID); err != nil {
			return pkg.NewError(i18n.T("cat.store"), err.Error(), pkg.ExitNetwork)
		}
		printRows("Workflows", [][2]string{{name, i18n.T("module.deleted")}})
		return nil
	},
}

func runModuleWorkflowList(cmd *cobra.Command, args []string) error {
	token, err := requireModuleToken(args)
	if err != nil {
		return err
	}
	client, err := connectedClient()
	if err != nil {
		return err
	}
	workflows, err := client.ListWorkflows(context.Background(), token)
	if err != nil {
		return pkg.NewError(i18n.T("cat.store"), err.Error(), pkg.ExitNetwork)
	}
	rows := make([][2]string, 0, len(workflows))
	for _, w := range workflows {
		rows = append(rows, [2]string{w.Name, fmt.Sprintf("%s · %s · %d exécutions", w.Source, w.Status, w.RunCount)})
	}
	printRows("Workflows", rows)
	return nil
}

func selectWorkflow(workflows []store.Workflow) (string, error) {
	if len(workflows) == 0 {
		return "", pkg.NewError(i18n.T("cat.selection"), i18n.T("module.error.no_workflows"), pkg.ExitError)
	}
	if len(workflows) == 1 {
		return workflows[0].ID, nil
	}
	if !tui.IsInteractive() {
		return "", pkg.NewError(i18n.T("cat.selection"), i18n.T("module.error.select_workflow"), pkg.ExitError)
	}
	labels := make([]string, 0, len(workflows))
	index := make(map[string]string, len(workflows))
	for _, w := range workflows {
		label := fmt.Sprintf("%s (%s)", w.Name, w.Status)
		labels = append(labels, label)
		index[label] = w.ID
	}
	selected, err := tui.Select(i18n.T("module.prompt.select_workflow"), labels)
	if err != nil {
		return "", err
	}
	return index[selected], nil
}

func workflowLabel(workflows []store.Workflow, id string) string {
	for _, w := range workflows {
		if w.ID == id {
			return w.Name
		}
	}
	return id
}

// --- channels --------------------------------------------------------------

var (
	flagChannelPublishVersion string
	flagChannelForce          bool
)

var moduleChannelsCmd = &cobra.Command{
	Use:   "channels [module]",
	Short: "Inspect and operate a module's build channels",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runModuleChannelsList,
}

var moduleChannelsListCmd = &cobra.Command{
	Use:   "list [module]",
	Short: "List a module's build channels",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runModuleChannelsList,
}

var moduleChannelsPublishCmd = &cobra.Command{
	Use:   "publish <channel> <version>",
	Short: "Publish a version on a build channel",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		token, err := requireModuleToken(nil)
		if err != nil {
			return err
		}
		client, err := connectedClient()
		if err != nil {
			return err
		}
		channel, err := normalizeChannel(args[0])
		if err != nil {
			return err
		}
		version := strings.TrimSpace(args[1])
		if !pkg.IsSemver(version) {
			return pkg.NewError(i18n.T("cat.input"), i18n.Tf("module.error.semver", version), pkg.ExitError)
		}
		result, err := client.PublishChannel(context.Background(), token, channel, version)
		if err != nil {
			return pkg.NewError(i18n.T("cat.store"), err.Error(), pkg.ExitNetwork)
		}
		printRows("Canaux de build", [][2]string{
			{result.Name, fmt.Sprintf("%s · %s", result.VersionString, result.Status)},
		})
		return nil
	},
}

var moduleChannelsRollbackCmd = &cobra.Command{
	Use:   "rollback <channel>",
	Short: "Rollback a build channel to its previous version",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		token, err := requireModuleToken(nil)
		if err != nil {
			return err
		}
		client, err := connectedClient()
		if err != nil {
			return err
		}
		channel, err := normalizeChannel(args[0])
		if err != nil {
			return err
		}
		result, err := client.RollbackChannel(context.Background(), token, channel)
		if err != nil {
			return pkg.NewError(i18n.T("cat.store"), err.Error(), pkg.ExitNetwork)
		}
		printRows("Canaux de build", [][2]string{
			{result.Name, fmt.Sprintf("%s · %s", result.VersionString, result.Status)},
		})
		return nil
	},
}

var moduleChannelsPauseCmd = &cobra.Command{
	Use:   "pause <channel>",
	Short: "Pause a build channel",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		token, err := requireModuleToken(nil)
		if err != nil {
			return err
		}
		client, err := connectedClient()
		if err != nil {
			return err
		}
		channel, err := normalizeChannel(args[0])
		if err != nil {
			return err
		}
		result, err := client.PauseChannel(context.Background(), token, channel)
		if err != nil {
			return pkg.NewError(i18n.T("cat.store"), err.Error(), pkg.ExitNetwork)
		}
		printRows("Canaux de build", [][2]string{
			{result.Name, fmt.Sprintf("%s · %s", result.VersionString, result.Status)},
		})
		return nil
	},
}

func runModuleChannelsList(cmd *cobra.Command, args []string) error {
	token, err := requireModuleToken(args)
	if err != nil {
		return err
	}
	client, err := connectedClient()
	if err != nil {
		return err
	}
	channels, err := client.ListBuildChannels(context.Background(), token)
	if err != nil {
		return pkg.NewError(i18n.T("cat.store"), err.Error(), pkg.ExitNetwork)
	}
	rows := make([][2]string, 0, len(channels))
	for _, c := range channels {
		version := c.VersionString
		if version == "" {
			version = "—"
		}
		rows = append(rows, [2]string{c.Name, fmt.Sprintf("%s · %s · %d mises à jour", version, c.Status, c.UpdateCount)})
	}
	printRows("Canaux de build", rows)
	return nil
}

// --- platforms -------------------------------------------------------------

var (
	flagPlatformWeb     bool
	flagPlatformDesktop bool
	flagPlatformMobile  bool
)

var modulePlatformsCmd = &cobra.Command{
	Use:   "platforms [module]",
	Short: "Show a module's supported platforms",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		token, err := requireModuleToken(args)
		if err != nil {
			return err
		}
		client, err := connectedClient()
		if err != nil {
			return err
		}
		platforms, err := client.GetModulePlatforms(context.Background(), token)
		if err != nil {
			return pkg.NewError(i18n.T("cat.store"), err.Error(), pkg.ExitNetwork)
		}
		printRows("Plateformes", platformRows(platforms))
		return nil
	},
}

var modulePlatformsSetCmd = &cobra.Command{
	Use:   "set [module]",
	Short: "Set a module's supported platforms",
	Long: `Replaces the module platform configuration. The supported flags
(--web, --desktop, --mobile) enable the matching platform; the others are marked
as unsupported. At least one flag is required.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if !flagPlatformWeb && !flagPlatformDesktop && !flagPlatformMobile {
			return pkg.NewErrorWithFix(i18n.T("cat.input"),
				i18n.T("module.error.platform_required"),
				i18n.T("module.error.platform_required.fix"), pkg.ExitError)
		}
		token, err := requireModuleToken(args)
		if err != nil {
			return err
		}
		client, err := connectedClient()
		if err != nil {
			return err
		}
		enabled := map[string]bool{
			"WEB":     flagPlatformWeb,
			"DESKTOP": flagPlatformDesktop,
			"MOBILE":  flagPlatformMobile,
		}
		platforms, err := client.GetModulePlatforms(context.Background(), token)
		if err != nil {
			return pkg.NewError(i18n.T("cat.store"), err.Error(), pkg.ExitNetwork)
		}
		for i := range platforms {
			supported := enabled[platforms[i].Platform]
			platforms[i].Supported = supported
			if supported && len(platforms[i].Modes) == 0 {
				platforms[i].Modes = []string{"DEFAULT"}
			}
			if !supported {
				platforms[i].Modes = nil
				platforms[i].Os = nil
			}
		}
		saved, err := client.SaveModulePlatforms(context.Background(), token, platforms)
		if err != nil {
			return pkg.NewError(i18n.T("cat.store"), err.Error(), pkg.ExitNetwork)
		}
		printRows("Plateformes", platformRows(saved))
		return nil
	},
}

func platformRows(platforms []store.PlatformSupport) [][2]string {
	rows := make([][2]string, 0, len(platforms))
	for _, p := range platforms {
		state := "non supporté"
		if p.Supported {
			state = strings.Join(p.Modes, ", ")
			if len(p.Os) > 0 {
				state += " · " + strings.Join(p.Os, ", ")
			}
			if state == "" {
				state = "supporté"
			}
		}
		rows = append(rows, [2]string{p.Platform, state})
	}
	return rows
}

// --- requirements ----------------------------------------------------------

var (
	flagRequirementModule  string
	flagRequirementVersion string
	flagRequirementKind    string
	flagRequirementReason  string
)

var moduleRequirementsCmd = &cobra.Command{
	Use:   "requirements [module]",
	Short: "List a module's required and optional dependencies",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runModuleRequirementsList,
}

var moduleRequirementsListCmd = &cobra.Command{
	Use:   "list [module]",
	Short: "List a module's required and optional dependencies",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runModuleRequirementsList,
}

var moduleRequirementsAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Declare a required or optional module dependency",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		moduleID := strings.TrimSpace(flagRequirementModule)
		if moduleID == "" {
			return pkg.NewErrorWithFix(i18n.T("cat.input"), i18n.T("module.error.requirement_module"),
				i18n.T("module.error.requirement_module.fix"), pkg.ExitError)
		}
		versionRange := strings.TrimSpace(flagRequirementVersion)
		if versionRange == "" {
			versionRange = ">=1.0.0"
		}
		token, err := requireModuleToken(nil)
		if err != nil {
			return err
		}
		client, err := connectedClient()
		if err != nil {
			return err
		}
		kind := strings.ToUpper(strings.TrimSpace(flagRequirementKind))
		if kind == "" {
			kind = "REQUIRED"
		}
		body := store.CreateRequirementRequest{
			ModuleID:     moduleID,
			Name:         strings.TrimSpace(args[0]),
			VersionRange: versionRange,
			Kind:         kind,
			Reason:       strings.TrimSpace(flagRequirementReason),
		}
		requirement, err := client.CreateRequirement(context.Background(), token, body)
		if err != nil {
			return pkg.NewError(i18n.T("cat.store"), err.Error(), pkg.ExitNetwork)
		}
		printRows("Modules requis & optionnels", [][2]string{
			{requirement.Name, fmt.Sprintf("%s %s (%s)", requirement.ModuleID, requirement.VersionRange, requirement.Kind)},
		})
		return nil
	},
}

func runModuleRequirementsList(cmd *cobra.Command, args []string) error {
	token, err := requireModuleToken(args)
	if err != nil {
		return err
	}
	client, err := connectedClient()
	if err != nil {
		return err
	}
	requirements, err := client.ListRequirements(context.Background(), token)
	if err != nil {
		return pkg.NewError(i18n.T("cat.store"), err.Error(), pkg.ExitNetwork)
	}
	rows := make([][2]string, 0, len(requirements))
	for _, r := range requirements {
		rows = append(rows, [2]string{r.Name, fmt.Sprintf("%s %s (%s)", r.ModuleID, r.VersionRange, r.Kind)})
	}
	printRows("Modules requis & optionnels", rows)
	return nil
}

// --- signing keys ----------------------------------------------------------

var flagSigningKeyForce bool

var moduleSigningKeysCmd = &cobra.Command{
	Use:   "signing-keys",
	Short: "List the account's signing keys",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := connectedClient()
		if err != nil {
			return err
		}
		keys, err := client.ListSigningKeys(context.Background())
		if err != nil {
			return pkg.NewError(i18n.T("cat.store"), err.Error(), pkg.ExitNetwork)
		}
		rows := make([][2]string, 0, len(keys))
		for _, k := range keys {
			rows = append(rows, [2]string{k.KeyID, fmt.Sprintf("%s · %s", k.Algorithm, k.Status)})
		}
		printRows("Clés de signature", rows)
		return nil
	},
}

var moduleSigningKeyRotateCmd = &cobra.Command{
	Use:   "rotate <keyId>",
	Short: "Rotate a signing key",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := connectedClient()
		if err != nil {
			return err
		}
		key, err := client.RotateSigningKey(context.Background(), strings.TrimSpace(args[0]))
		if err != nil {
			return pkg.NewError(i18n.T("cat.store"), err.Error(), pkg.ExitNetwork)
		}
		printRows("Clés de signature", [][2]string{
			{key.KeyID, fmt.Sprintf("%s · %s", key.Algorithm, key.Status)},
		})
		return nil
	},
}

// --- accreditations --------------------------------------------------------

var (
	flagAccreditationKind      string
	flagAccreditationReference string
)

var moduleAccreditationsCmd = &cobra.Command{
	Use:   "accreditations",
	Short: "Manage the account's accreditations",
	Args:  cobra.NoArgs,
	RunE:  runModuleAccreditationsList,
}

var moduleAccreditationsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List the account's accreditations",
	Args:  cobra.NoArgs,
	RunE:  runModuleAccreditationsList,
}

var moduleAccreditationsAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Register an account accreditation",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		kind := strings.ToUpper(strings.TrimSpace(flagAccreditationKind))
		if kind == "" {
			kind = "CI_CD_TOKEN"
		}
		client, err := connectedClient()
		if err != nil {
			return err
		}
		body := store.CreateAccreditationRequest{
			Kind:      kind,
			Name:      strings.TrimSpace(args[0]),
			Reference: strings.TrimSpace(flagAccreditationReference),
		}
		accreditation, err := client.CreateAccreditation(context.Background(), body)
		if err != nil {
			return pkg.NewError(i18n.T("cat.store"), err.Error(), pkg.ExitNetwork)
		}
		printRows("Accréditations", [][2]string{
			{accreditation.Name, fmt.Sprintf("%s · %s", accreditation.Kind, accreditation.Status)},
		})
		return nil
	},
}

func runModuleAccreditationsList(cmd *cobra.Command, args []string) error {
	client, err := connectedClient()
	if err != nil {
		return err
	}
	accreditations, err := client.ListAccreditations(context.Background())
	if err != nil {
		return pkg.NewError(i18n.T("cat.store"), err.Error(), pkg.ExitNetwork)
	}
	rows := make([][2]string, 0, len(accreditations))
	for _, a := range accreditations {
		rows = append(rows, [2]string{a.Name, fmt.Sprintf("%s · %s", a.Kind, a.Status)})
	}
	printRows("Accréditations", rows)
	return nil
}

// --- environment variables -------------------------------------------------

var (
	flagVariableValue        string
	flagVariableEnvironments []string
	flagVariableVisibility   string
	flagVariableKey          string
	flagVariableModule       string
	flagVariableForce        bool
)

var moduleVariablesCmd = &cobra.Command{
	Use:   "variables [module]",
	Short: "Manage environment variables (module or shared)",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runModuleVariablesList,
}

var moduleVariablesListCmd = &cobra.Command{
	Use:   "list [module]",
	Short: "List environment variables (module or shared)",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runModuleVariablesList,
}

var moduleVariablesSetCmd = &cobra.Command{
	Use:   "set <key>",
	Short: "Create or update an environment variable",
	Long: `Creates the environment variable when it does not exist yet, otherwise
updates it. The value is read from --value or from an interactive prompt.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := strings.ToUpper(strings.TrimSpace(args[0]))
		client, err := connectedClient()
		if err != nil {
			return err
		}
		moduleToken := ""
		if len(flagVariableModule) > 0 {
			token, err := requireModuleToken([]string{flagVariableModule})
			if err != nil {
				return err
			}
			moduleToken = token
		}
		value := flagVariableValue
		if !cmd.Flags().Changed("value") {
			value, err = tui.AskSecret(fmt.Sprintf(i18n.T("module.prompt.variable_value"), key))
			if err != nil {
				return err
			}
		}
		environments := flagVariableEnvironments
		if len(environments) == 0 {
			environments = []string{"DEVELOPMENT", "STAGING", "PRODUCTION"}
		}
		body := store.SaveEnvironmentVariableRequest{
			Key:          key,
			Value:        value,
			Environments: environments,
			Visibility:   strings.ToUpper(strings.TrimSpace(flagVariableVisibility)),
			ProductID:    moduleToken,
		}
		existing, err := client.ListEnvironmentVariables(context.Background(), moduleToken)
		if err != nil {
			return pkg.NewError(i18n.T("cat.store"), err.Error(), pkg.ExitNetwork)
		}
		current := findVariable(existing, key)
		var variable *store.EnvironmentVariable
		if current != nil {
			variable, err = client.UpdateEnvironmentVariable(context.Background(), current.ID, body)
		} else {
			variable, err = client.CreateEnvironmentVariable(context.Background(), body)
		}
		if err != nil {
			return pkg.NewError(i18n.T("cat.store"), err.Error(), pkg.ExitNetwork)
		}
		printRows("Variables d'environnement", [][2]string{
			{variable.Key, fmt.Sprintf("%s · %s", strings.Join(variable.Environments, ", "), variable.Visibility)},
		})
		return nil
	},
}

var moduleVariablesUnsetCmd = &cobra.Command{
	Use:   "unset <key>",
	Short: "Delete an environment variable",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := strings.ToUpper(strings.TrimSpace(args[0]))
		client, err := connectedClient()
		if err != nil {
			return err
		}
		moduleToken := ""
		if len(flagVariableModule) > 0 {
			token, err := requireModuleToken([]string{flagVariableModule})
			if err != nil {
				return err
			}
			moduleToken = token
		}
		variables, err := client.ListEnvironmentVariables(context.Background(), moduleToken)
		if err != nil {
			return pkg.NewError(i18n.T("cat.store"), err.Error(), pkg.ExitNetwork)
		}
		current := findVariable(variables, key)
		if current == nil {
			return pkg.NewError(i18n.T("cat.module"), i18n.Tf("module.error.variable_absent", key), pkg.ExitModuleNotFound)
		}
		if !flagVariableForce {
			ok, err := tui.Confirm(fmt.Sprintf(i18n.T("module.prompt.confirm_delete"), key), false)
			if err != nil {
				return err
			}
			if !ok {
				return nil
			}
		}
		if err := client.DeleteEnvironmentVariable(context.Background(), current.ID); err != nil {
			return pkg.NewError(i18n.T("cat.store"), err.Error(), pkg.ExitNetwork)
		}
		printRows("Variables d'environnement", [][2]string{{key, i18n.T("module.deleted")}})
		return nil
	},
}

func runModuleVariablesList(cmd *cobra.Command, args []string) error {
	moduleToken := ""
	if len(args) > 0 {
		token, err := requireModuleToken(args)
		if err != nil {
			return err
		}
		moduleToken = token
	}
	client, err := connectedClient()
	if err != nil {
		return err
	}
	variables, err := client.ListEnvironmentVariables(context.Background(), moduleToken)
	if err != nil {
		return pkg.NewError(i18n.T("cat.store"), err.Error(), pkg.ExitNetwork)
	}
	rows := make([][2]string, 0, len(variables))
	for _, v := range variables {
		rows = append(rows, [2]string{v.Key, fmt.Sprintf("%s · %s", strings.Join(v.Environments, ", "), v.Visibility)})
	}
	printRows("Variables d'environnement", rows)
	return nil
}

func findVariable(variables []store.EnvironmentVariable, key string) *store.EnvironmentVariable {
	for i := range variables {
		if strings.EqualFold(variables[i].Key, key) {
			return &variables[i]
		}
	}
	return nil
}

// --- github ----------------------------------------------------------------

var (
	flagGithubConnect      bool
	flagGithubRepository   string
	flagGithubBranch       string
	flagGithubWorkflowPath string
	flagGithubModule       string
)

var moduleGithubCmd = &cobra.Command{
	Use:   "github [module]",
	Short: "Show or connect the GitHub repository binding",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		moduleToken := ""
		if len(args) > 0 {
			token, err := requireModuleToken(args)
			if err != nil {
				return err
			}
			moduleToken = token
		}
		client, err := connectedClient()
		if err != nil {
			return err
		}
		if flagGithubConnect {
			return linkGithub(client, moduleToken)
		}
		connection, err := client.GetGithubConnection(context.Background(), moduleToken)
		if err != nil {
			return pkg.NewError(i18n.T("cat.store"), err.Error(), pkg.ExitNetwork)
		}
		if connection == nil || !connection.Connected {
			printRows("GitHub", nil)
			return nil
		}
		printRows("GitHub", [][2]string{
			{"Dépôt", connection.Repository},
			{"Branche", connection.DefaultBranch},
			{"Workflow", connection.WorkflowPath},
			{"Dernier statut", connection.LastWorkflowStatus},
		})
		return nil
	},
}

var moduleGithubLinkCmd = &cobra.Command{
	Use:   "link [module]",
	Short: "Link a GitHub repository to the account or a module",
	Long: `Links a GitHub repository. --repository is required in non-interactive
shells; otherwise the repository is prompted interactively.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		moduleToken := ""
		if len(args) > 0 {
			token, err := requireModuleToken(args)
			if err != nil {
				return err
			}
			moduleToken = token
		}
		client, err := connectedClient()
		if err != nil {
			return err
		}
		return linkGithub(client, moduleToken)
	},
}

var moduleGithubUnlinkCmd = &cobra.Command{
	Use:   "unlink [module]",
	Short: "Remove the GitHub repository binding",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		moduleToken := ""
		if len(args) > 0 {
			token, err := requireModuleToken(args)
			if err != nil {
				return err
			}
			moduleToken = token
		}
		client, err := connectedClient()
		if err != nil {
			return err
		}
		if err := client.DisconnectGithub(context.Background(), moduleToken); err != nil {
			return pkg.NewError(i18n.T("cat.store"), err.Error(), pkg.ExitNetwork)
		}
		printRows("GitHub", [][2]string{{"Connexion", i18n.T("module.deleted")}})
		return nil
	},
}

func linkGithub(client *store.Client, moduleToken string) error {
	repository := strings.TrimSpace(flagGithubRepository)
	if repository == "" {
		prompted, err := tui.AskText("Dépôt (owner/repo)", "")
		if err != nil {
			return err
		}
		repository = strings.TrimSpace(prompted)
	}
	branch := strings.TrimSpace(flagGithubBranch)
	if branch == "" {
		branch = "main"
	}
	connection, err := client.ConnectGithub(context.Background(), repository, branch,
		strings.TrimSpace(flagGithubWorkflowPath), moduleToken)
	if err != nil {
		return pkg.NewError(i18n.T("cat.store"), err.Error(), pkg.ExitNetwork)
	}
	printRows("GitHub", [][2]string{
		{connection.Repository, fmt.Sprintf("%s · workflow %s", connection.DefaultBranch, connection.WorkflowPath)},
	})
	return nil
}

// --- observer & usage ------------------------------------------------------

var moduleObserverCmd = &cobra.Command{
	Use:   "observer [module]",
	Short: "Show a module's observability metrics",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		token, err := requireModuleToken(args)
		if err != nil {
			return err
		}
		client, err := connectedClient()
		if err != nil {
			return err
		}
		observer, err := client.GetObserver(context.Background(), token)
		if err != nil {
			return pkg.NewError(i18n.T("cat.store"), err.Error(), pkg.ExitNetwork)
		}
		rows := make([][2]string, 0, len(observer.Metrics))
		for _, m := range observer.Metrics {
			rows = append(rows, [2]string{m.Label, fmt.Sprintf("%s (%s) — %s", m.Value, m.Trend, m.Hint)})
		}
		printRows("Observer", rows)
		return nil
	},
}

var moduleUsageCmd = &cobra.Command{
	Use:   "usage [module]",
	Short: "Show a module's usage series",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		token, err := requireModuleToken(args)
		if err != nil {
			return err
		}
		client, err := connectedClient()
		if err != nil {
			return err
		}
		usage, err := client.GetUsage(context.Background(), token)
		if err != nil {
			return pkg.NewError(i18n.T("cat.store"), err.Error(), pkg.ExitNetwork)
		}
		rows := make([][2]string, 0, len(usage.Series)+1)
		for _, point := range usage.Series {
			rows = append(rows, [2]string{point.Label, fmt.Sprintf("%d installations · %d activations", point.Installations, point.Activations)})
		}
		rows = append(rows, [2]string{"Totaux", fmt.Sprintf("%d installations · %d activations · %d organisations actives",
			usage.Totals.Installations, usage.Totals.Activations, usage.Totals.ActiveOrganizations)})
		printRows("Usage", rows)
		return nil
	},
}

// --- helpers ---------------------------------------------------------------

// buildChannels is the ordered set of distribution channels exposed by the API.
var buildChannels = []string{"ALPHA", "BETA", "NIGHTLY", "RC", "RELEASE"}

// normalizeChannel upper-cases and validates a channel name.
func normalizeChannel(name string) (string, error) {
	channel := strings.ToUpper(strings.TrimSpace(name))
	for _, c := range buildChannels {
		if c == channel {
			return c, nil
		}
	}
	return "", pkg.NewErrorWithFix(i18n.T("cat.input"),
		i18n.Tf("module.error.channel", name),
		fmt.Sprintf("Canaux valides : %s.", strings.Join(buildChannels, ", ")), pkg.ExitError)
}

// slugify converts a title into a kebab-case slug, keeping ASCII letters,
// digits and single dashes.

func init() {
	moduleKnowledgeCmd.AddCommand(moduleKnowledgeListCmd, moduleKnowledgeAddCmd, moduleKnowledgePublishCmd)
	moduleKnowledgeAddCmd.Flags().StringVar(&flagKnowledgeKind, "kind", "GUIDE", "article kind (GUIDE|REFERENCE|CONCEPT)")
	moduleKnowledgeAddCmd.Flags().StringVar(&flagKnowledgeSummary, "summary", "", "short article summary")
	moduleKnowledgeAddCmd.Flags().IntVar(&flagKnowledgeReading, "reading-minutes", 1, "estimated reading time in minutes")
	moduleKnowledgeAddCmd.Flags().BoolVar(&flagKnowledgePublish, "publish", false, "publish immediately")
	moduleKnowledgeAddCmd.Flags().BoolVar(&flagKnowledgeAsDraft, "draft", false, "force draft even with --publish")
	moduleKnowledgePublishCmd.Flags().StringVar(&flagKnowledgeArticleID, "id", "", "article id (skips the interactive selection)")
	moduleKnowledgePublishCmd.Flags().BoolVar(&flagKnowledgeAsDraft, "draft", false, "flip the article back to draft")

	moduleWorkflowsCmd.AddCommand(moduleWorkflowListCmd, moduleWorkflowRunCmd, moduleWorkflowDeleteCmd)
	moduleWorkflowRunCmd.Flags().StringVar(&flagWorkflowRunID, "id", "", "workflow id (skips the interactive selection)")
	moduleWorkflowDeleteCmd.Flags().StringVar(&flagWorkflowRunID, "id", "", "workflow id (skips the interactive selection)")
	moduleWorkflowDeleteCmd.Flags().BoolVarP(&flagWorkflowForce, "force", "f", false, "skip the confirmation prompt")

	moduleChannelsCmd.AddCommand(moduleChannelsListCmd, moduleChannelsPublishCmd,
		moduleChannelsRollbackCmd, moduleChannelsPauseCmd)

	modulePlatformsCmd.AddCommand(modulePlatformsSetCmd)
	modulePlatformsSetCmd.Flags().BoolVar(&flagPlatformWeb, "web", false, "support the web platform")
	modulePlatformsSetCmd.Flags().BoolVar(&flagPlatformDesktop, "desktop", false, "support the desktop platform")
	modulePlatformsSetCmd.Flags().BoolVar(&flagPlatformMobile, "mobile", false, "support the mobile platform")

	moduleRequirementsCmd.AddCommand(moduleRequirementsListCmd, moduleRequirementsAddCmd)
	moduleRequirementsAddCmd.Flags().StringVar(&flagRequirementModule, "module-id", "", "canonical identifier of the required module")
	moduleRequirementsAddCmd.Flags().StringVar(&flagRequirementVersion, "version", ">=1.0.0", "accepted version range")
	moduleRequirementsAddCmd.Flags().StringVar(&flagRequirementKind, "kind", "REQUIRED", "dependency kind (REQUIRED|OPTIONAL)")
	moduleRequirementsAddCmd.Flags().StringVar(&flagRequirementReason, "reason", "", "why the dependency is required")

	moduleSigningKeysCmd.AddCommand(moduleSigningKeyRotateCmd)

	moduleAccreditationsCmd.AddCommand(moduleAccreditationsListCmd, moduleAccreditationsAddCmd)
	moduleAccreditationsAddCmd.Flags().StringVar(&flagAccreditationKind, "kind", "CI_CD_TOKEN", "accreditation kind (CI_CD_TOKEN|SIGNING_KEY|STORE_ACCOUNT)")
	moduleAccreditationsAddCmd.Flags().StringVar(&flagAccreditationReference, "reference", "", "external reference")

	moduleVariablesCmd.AddCommand(moduleVariablesListCmd, moduleVariablesSetCmd, moduleVariablesUnsetCmd)
	moduleVariablesSetCmd.Flags().StringVar(&flagVariableKey, "key", "", "variable key (alternative to the positional argument)")
	moduleVariablesSetCmd.Flags().StringVar(&flagVariableValue, "value", "", "variable value (prompted when absent)")
	moduleVariablesSetCmd.Flags().StringSliceVar(&flagVariableEnvironments, "env", nil, "target environments (DEVELOPMENT|STAGING|PRODUCTION)")
	moduleVariablesSetCmd.Flags().StringVar(&flagVariableVisibility, "visibility", "MASKED", "visibility (PLAIN_TEXT|MASKED)")
	moduleVariablesSetCmd.Flags().StringVar(&flagVariableModule, "module", "", "attach the variable to a module")
	moduleVariablesUnsetCmd.Flags().StringVar(&flagVariableModule, "module", "", "module the variable belongs to")
	moduleVariablesUnsetCmd.Flags().BoolVarP(&flagVariableForce, "force", "f", false, "skip the confirmation prompt")

	moduleGithubCmd.AddCommand(moduleGithubLinkCmd, moduleGithubUnlinkCmd)
	moduleGithubCmd.Flags().BoolVar(&flagGithubConnect, "connect", false, "link a GitHub repository interactively (legacy alias of link)")
	moduleGithubLinkCmd.Flags().StringVar(&flagGithubRepository, "repository", "", "repository to link (owner/repo)")
	moduleGithubLinkCmd.Flags().StringVar(&flagGithubBranch, "branch", "main", "default branch")
	moduleGithubLinkCmd.Flags().StringVar(&flagGithubWorkflowPath, "workflow", "", "relative path of the CI workflow")
}
