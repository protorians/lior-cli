package module

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/protorians/lior-cli/internal/config"
)

// Conformity limits of a `module.webhooks.json` declaration (spec
// docs/specs/modules/module-webhooks/features/webhook-registry.md,
// G-04..G-06). They are enforced locally so a non-conforming declaration
// never leaves the machine — pack, `liora module webhook push` and
// `liora publish` all refuse before any network call.
const (
	// WebhooksSchemaVersion is the only supported `schemaVersion` (G-06).
	WebhooksSchemaVersion = 1
	// MaxWebhookActions bounds the `actions` list (G-05).
	MaxWebhookActions = 50
	// MaxWebhookSubscriptions bounds the `subscriptions` list (G-05).
	MaxWebhookSubscriptions = 20
	// MaxWebhookDescription bounds every `description` field (G-05).
	MaxWebhookDescription = 300
	// MaxWebhookPayloadSize bounds the JSON size of one action `payload`
	// schema (G-04: ≤ 8 Ko).
	MaxWebhookPayloadSize = 8 * 1024
)

// WebhooksDeclaration models `module.webhooks.json` — the webhooks
// declaration file of a module, at the root of its directory next to the
// manifest (spec `module-declaration.md` §1). Like the manifest, the document
// is kept verbatim in the artefact; the Go projection here only serves the
// local validation (G-01..G-07).
type WebhooksDeclaration struct {
	Schema        string                           `json:"$schema,omitempty"`
	SchemaVersion int                              `json:"schemaVersion"`
	Version       string                           `json:"version"`
	Actions       []WebhookActionDeclaration       `json:"actions"`
	Subscriptions []WebhookSubscriptionDeclaration `json:"subscriptions,omitempty"`
}

// WebhookActionDeclaration is one entry of `actions`: a webhook the module
// emits. `action` is the canonical key `module:tache.nom-action` (G-01);
// `payload` is the JSON schema of the emitted payload (G-04).
type WebhookActionDeclaration struct {
	Action      string          `json:"action"`
	Task        string          `json:"task"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Payload     json.RawMessage `json:"payload,omitempty"`
}

// WebhookSubscriptionDeclaration is one entry of `subscriptions`: a webhook
// action of ANOTHER module the module subscribes to (G-03 — a module never
// subscribes to its own actions).
type WebhookSubscriptionDeclaration struct {
	Action      string `json:"action"`
	Description string `json:"description,omitempty"`
}

// WebhooksDeclarationPath returns the path of the webhooks declaration file at
// the root of a module directory (next to `manifest.json`).
func WebhooksDeclarationPath(moduleDir string) string {
	return filepath.Join(moduleDir, config.WebhooksFileName)
}

// LoadWebhooksDeclaration reads `module.webhooks.json` at the root of a module
// directory. An absent file is not an error: the declaration is optional and
// the loader returns `(nil, nil)` — the packer, the push command and the
// publish relay all treat nil as "no declared webhook" (G-07).
func LoadWebhooksDeclaration(moduleDir string) (*WebhooksDeclaration, error) {
	path := WebhooksDeclarationPath(moduleDir)
	document, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("échec de lecture de %s : %w", path, err)
	}
	var declaration WebhooksDeclaration
	if err := json.Unmarshal(document, &declaration); err != nil {
		return nil, fmt.Errorf("échec du décodage JSON de %s : %w", path, err)
	}
	return &declaration, nil
}

// webhookActionRE matches a canonical webhook action key (G-01):
// `^<module-key>:<tache>.<nom-action>` — module name in kebab-case and
// task/action names in `[a-z][a-z0-9-]{1,63}`. This is the same grammar the
// registry service re-enforces on reception (`webhook-action-key.ts`).
var webhookActionRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{2,63}:[a-z][a-z0-9-]{1,63}\.[a-z][a-z0-9-]{1,63}$`)

// splitWebhookActionKey decomposes a canonical action key into its
// `<module-key>`, `<tache>` and `<nom-action>` segments. ok=false when the
// separator grammar does not hold (the regex already guarantees it, but the
// decomposition guards the coherence rule below).
func splitWebhookActionKey(action string) (moduleKey, task, name string, ok bool) {
	moduleKey, rest, found := strings.Cut(action, ":")
	if !found {
		return "", "", "", false
	}
	task, name, found = strings.Cut(rest, ".")
	if !found {
		return "", "", "", false
	}
	return moduleKey, task, name, true
}

// ValidateWebhooksDeclaration checks a declaration against G-01..G-07. The
// errors are blocking at pack, at `liora module webhook push` and at the
// `liora publish` relay — a declaration must be fixed locally before it can
// reach the registry.
//
// `manifestID` is the module identifier of the declaring module's manifest
// (G-02 anti-usurpation: an action prefix equal to another module's
// identifier is refused). An empty manifestID skips the ownership check
// (nothing to compare against; the command callers always pass one).
func ValidateWebhooksDeclaration(d *WebhooksDeclaration, manifestID string) error {
	if d == nil {
		return nil
	}
	if d.SchemaVersion != WebhooksSchemaVersion {
		return fmt.Errorf("schemaVersion %d non pris en charge (attendu : %d)",
			d.SchemaVersion, WebhooksSchemaVersion)
	}
	if !isSemver(strings.TrimSpace(d.Version)) {
		return fmt.Errorf("version %q du fichier %s invalide (SemVer attendu)",
			d.Version, config.WebhooksFileName)
	}
	if len(d.Actions) > MaxWebhookActions {
		return fmt.Errorf("la déclaration dépasse le quota d'actions (%d : %d)",
			MaxWebhookActions, len(d.Actions))
	}
	if len(d.Subscriptions) > MaxWebhookSubscriptions {
		return fmt.Errorf("la déclaration dépasse le quota de subscriptions (%d : %d)",
			MaxWebhookSubscriptions, len(d.Subscriptions))
	}

	seen := make(map[string]bool, len(d.Actions))
	for _, action := range d.Actions {
		if err := validateWebhookAction(action, manifestID, seen); err != nil {
			return err
		}
	}
	for _, subscription := range d.Subscriptions {
		if err := validateWebhookSubscription(subscription, manifestID); err != nil {
			return err
		}
	}
	return nil
}

// validateWebhookAction checks one `actions[]` entry: canonical key (G-01),
// unique key, ownership (G-02), `task`/`name` coherence, description (G-05)
// and payload size (G-04).
func validateWebhookAction(a WebhookActionDeclaration, manifestID string, seen map[string]bool) error {
	action := strings.TrimSpace(a.Action)
	if action == "" {
		return errors.New("action vide : la clé canonique « module:tache.nom-action » est requise")
	}
	if !webhookActionRE.MatchString(action) {
		return fmt.Errorf("action %q invalide : grammaire « module:tache.nom-action » attendue", action)
	}
	if seen[action] {
		return fmt.Errorf("action dupliquée dans la déclaration : %s", action)
	}
	seen[action] = true

	moduleKey, task, name, ok := splitWebhookActionKey(action)
	if !ok {
		return fmt.Errorf("action %q indécomposable", action)
	}
	if manifestID := strings.TrimSpace(manifestID); manifestID != "" && moduleKey != manifestID {
		return fmt.Errorf("un module ne peut déclarer que ses propres actions : %s n'appartient pas au manifeste %q",
			action, manifestID)
	}
	// The structured fields must agree with the canonical key when provided.
	if taskName := strings.TrimSpace(a.Task); taskName != "" && taskName != task {
		return fmt.Errorf("action %s : le champ task %q ne correspond pas à la tâche %q de la clé canonique",
			action, taskName, task)
	}
	if actionName := strings.TrimSpace(a.Name); actionName != "" && actionName != name {
		return fmt.Errorf("action %s : le champ name %q ne correspond pas au nom %q de la clé canonique",
			action, actionName, name)
	}
	if len(a.Description) > MaxWebhookDescription {
		return fmt.Errorf("action %s : description trop longue (%d caractères, maximum %d)",
			action, len(a.Description), MaxWebhookDescription)
	}
	if len(a.Payload) > MaxWebhookPayloadSize {
		return fmt.Errorf("action %s : payload trop volumineux (%d octets, maximum %d)",
			action, len(a.Payload), MaxWebhookPayloadSize)
	}
	return nil
}

// validateWebhookSubscription checks one `subscriptions[]` entry: canonical
// key (G-01) belonging to ANOTHER module (G-03 — no self-subscription) and a
// bounded description (G-05).
func validateWebhookSubscription(s WebhookSubscriptionDeclaration, manifestID string) error {
	action := strings.TrimSpace(s.Action)
	if action == "" {
		return errors.New("subscription vide : la clé canonique de l'action souscrite est requise")
	}
	if !webhookActionRE.MatchString(action) {
		return fmt.Errorf("subscription %q invalide : grammaire « module:tache.nom-action » attendue", action)
	}
	if manifestID := strings.TrimSpace(manifestID); manifestID != "" {
		moduleKey, _, _, ok := splitWebhookActionKey(action)
		if !ok {
			return fmt.Errorf("subscription %q indécomposable", action)
		}
		if moduleKey == manifestID {
			return fmt.Errorf("un module ne peut pas s'abonner à ses propres actions : %s", action)
		}
	}
	if len(s.Description) > MaxWebhookDescription {
		return fmt.Errorf("subscription %s : description trop longue (%d caractères, maximum %d)",
			action, len(s.Description), MaxWebhookDescription)
	}
	return nil
}
