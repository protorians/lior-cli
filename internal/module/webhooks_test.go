package module

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/protorians/lior-cli/internal/config"
)

// validWebhooksDeclaration is the reference declaration (spec
// docs/specs/modules/module-webhooks/features/webhook-registry.md).
func validWebhooksDeclaration() *WebhooksDeclaration {
	return &WebhooksDeclaration{
		Schema:        "@liorian/sdk/schemas/module.webhooks.schema.json",
		SchemaVersion: WebhooksSchemaVersion,
		Version:       "1.0.0",
		Actions: []WebhookActionDeclaration{
			{
				Action:      "crm:customer.created",
				Task:        "customer",
				Name:        "created",
				Description: "Émise quand un client est créé.",
				Payload:     []byte(`{"type":"object","properties":{"customerId":{"type":"string"}},"required":["customerId"]}`),
			},
			{
				Action: "crm:deal.won",
				Task:   "deal",
				Name:   "won",
			},
		},
		Subscriptions: []WebhookSubscriptionDeclaration{
			{Action: "blogging:post.published", Description: "Suivre la publication d'articles."},
		},
	}
}

func TestValidateWebhooksDeclarationAcceptsValid(t *testing.T) {
	if err := ValidateWebhooksDeclaration(validWebhooksDeclaration(), "crm"); err != nil {
		t.Fatalf("déclaration valide refusée : %v", err)
	}
}

func TestValidateWebhooksDeclarationAcceptsEmptyActions(t *testing.T) {
	// G-07 : `actions` peut être vide (module sans webhook).
	decl := &WebhooksDeclaration{SchemaVersion: WebhooksSchemaVersion, Version: "1.0.0"}
	if err := ValidateWebhooksDeclaration(decl, "crm"); err != nil {
		t.Fatalf("déclaration sans action refusée : %v", err)
	}
}

func TestValidateWebhooksDeclarationRefusesWrongSchemaVersion(t *testing.T) {
	decl := validWebhooksDeclaration()
	decl.SchemaVersion = 2
	err := ValidateWebhooksDeclaration(decl, "crm")
	if err == nil || !strings.Contains(err.Error(), "schemaVersion") {
		t.Fatalf("schemaVersion 2 acceptée : %v", err)
	}
}

func TestValidateWebhooksDeclarationRefusesNonSemverVersion(t *testing.T) {
	decl := validWebhooksDeclaration()
	decl.Version = "1.0"
	err := ValidateWebhooksDeclaration(decl, "crm")
	if err == nil || !strings.Contains(err.Error(), "SemVer") {
		t.Fatalf("version non-SemVer acceptée : %v", err)
	}
}

func TestValidateWebhooksDeclarationRefusesActionUsurpation(t *testing.T) {
	// G-02 : le préfixe de chaque action doit égaler l'id du manifeste.
	decl := validWebhooksDeclaration()
	decl.Actions[0].Action = "blogging:customer.created"
	err := ValidateWebhooksDeclaration(decl, "crm")
	if err == nil || !strings.Contains(err.Error(), "blogging:customer.created") {
		t.Fatalf("usurpation de préfixe acceptée : %v", err)
	}
}

func TestValidateWebhooksDeclarationRefusesBadGrammar(t *testing.T) {
	cases := map[string]string{
		"CRM:customer.created":          "module en majuscules",
		"crm:Customer.created":          "tâche en majuscules",
		"crm:customer.Created":          "nom-action en majuscules",
		"crm: customer.created":         "espace parasite",
		"crm:customer..created":         "segment vide",
		"crm:customer.customer.created": "segment supplémentaire",
		"crm:c.created":                 "tâche d'un seul caractère",
		"ab:customer.created":           "module trop court",
	}
	for action, reason := range cases {
		decl := validWebhooksDeclaration()
		decl.Actions[0].Action = action
		decl.Actions[0].Task = ""
		decl.Actions[0].Name = ""
		if err := ValidateWebhooksDeclaration(decl, "crm"); err == nil {
			t.Errorf("grammaire acceptée (%s) : %s", reason, action)
		}
	}
}

func TestValidateWebhooksDeclarationRefusesDuplicateAction(t *testing.T) {
	// G-01 : clé canonique unique dans la déclaration.
	decl := validWebhooksDeclaration()
	decl.Actions[1] = decl.Actions[0]
	err := ValidateWebhooksDeclaration(decl, "crm")
	if err == nil || !strings.Contains(err.Error(), "dupliquée") {
		t.Fatalf("action dupliquée acceptée : %v", err)
	}
}

func TestValidateWebhooksDeclarationRefusesTaskNameMismatch(t *testing.T) {
	decl := validWebhooksDeclaration()
	decl.Actions[0].Task = "deal"
	err := ValidateWebhooksDeclaration(decl, "crm")
	if err == nil || !strings.Contains(err.Error(), "task") {
		t.Fatalf("task incohérent accepté : %v", err)
	}

	decl = validWebhooksDeclaration()
	decl.Actions[0].Name = "created-v2"
	err = ValidateWebhooksDeclaration(decl, "crm")
	if err == nil || !strings.Contains(err.Error(), "name") {
		t.Fatalf("name incohérent accepté : %v", err)
	}
}

func TestValidateWebhooksDeclarationRefusesSelfSubscription(t *testing.T) {
	// G-03 : un module ne s'abonne pas à ses propres actions.
	decl := validWebhooksDeclaration()
	decl.Subscriptions[0].Action = "crm:deal.won"
	err := ValidateWebhooksDeclaration(decl, "crm")
	if err == nil || !strings.Contains(err.Error(), "s'abonner") {
		t.Fatalf("auto-abonnement accepté : %v", err)
	}
}

func TestValidateWebhooksDeclarationRefusesActionQuota(t *testing.T) {
	decl := validWebhooksDeclaration()
	decl.Actions = nil
	for i := 0; i <= MaxWebhookActions; i++ {
		decl.Actions = append(decl.Actions, WebhookActionDeclaration{
			Action: "crm:customer.event-" + itoaForTest(i),
		})
	}
	err := ValidateWebhooksDeclaration(decl, "crm")
	if err == nil || !strings.Contains(err.Error(), "quota d'actions") {
		t.Fatalf("quota d'actions non appliqué : %v", err)
	}
}

func TestValidateWebhooksDeclarationRefusesSubscriptionQuota(t *testing.T) {
	decl := validWebhooksDeclaration()
	decl.Subscriptions = nil
	for i := 0; i <= MaxWebhookSubscriptions; i++ {
		decl.Subscriptions = append(decl.Subscriptions, WebhookSubscriptionDeclaration{
			Action: "blogging:post.published-" + itoaForTest(i),
		})
	}
	err := ValidateWebhooksDeclaration(decl, "crm")
	if err == nil || !strings.Contains(err.Error(), "quota de subscriptions") {
		t.Fatalf("quota de subscriptions non appliqué : %v", err)
	}
}

func TestValidateWebhooksDeclarationRefusesLongDescription(t *testing.T) {
	decl := validWebhooksDeclaration()
	decl.Actions[0].Description = strings.Repeat("a", MaxWebhookDescription+1)
	err := ValidateWebhooksDeclaration(decl, "crm")
	if err == nil || !strings.Contains(err.Error(), "description") {
		t.Fatalf("description trop longue acceptée : %v", err)
	}
}

func TestValidateWebhooksDeclarationRefusesOversizedPayload(t *testing.T) {
	deck := strings.Repeat(`{"type":"string"},`, (MaxWebhookPayloadSize/len(`{"type":"string"},`))+1)
	decl := validWebhooksDeclaration()
	decl.Actions[0].Payload = []byte(`{"properties":[` + deck[:MaxWebhookPayloadSize+8] + `]}`)
	err := ValidateWebhooksDeclaration(decl, "crm")
	if err == nil || !strings.Contains(err.Error(), "payload") {
		t.Fatalf("payload surdimentionné accepté : %v", err)
	}
}

func TestValidateWebhooksDeclarationNilIsNeutral(t *testing.T) {
	if err := ValidateWebhooksDeclaration(nil, "crm"); err != nil {
		t.Fatalf("déclaration absente traitée en erreur : %v", err)
	}
}

func TestLoadWebhooksDeclarationAbsentIsNil(t *testing.T) {
	declaration, err := LoadWebhooksDeclaration(t.TempDir())
	if err != nil {
		t.Fatalf("absence traitée en erreur : %v", err)
	}
	if declaration != nil {
		t.Fatalf("absence traitée comme présente : %+v", declaration)
	}
}

func TestLoadWebhooksDeclarationRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, config.WebhooksFileName)
	document := `{
	  "schemaVersion": 1,
	  "version": "1.2.0",
	  "actions": [{"action": "crm:customer.created", "task": "customer", "name": "created"}]
	}`
	if err := os.WriteFile(path, []byte(document), 0o644); err != nil {
		t.Fatalf("écriture du test : %v", err)
	}
	declaration, err := LoadWebhooksDeclaration(dir)
	if err != nil {
		t.Fatalf("chargement : %v", err)
	}
	if declaration.Version != "1.2.0" || declaration.SchemaVersion != 1 || len(declaration.Actions) != 1 {
		t.Fatalf("contenu inattendu : %+v", declaration)
	}
	if declaration.Actions[0].Action != "crm:customer.created" || declaration.Actions[0].Task != "customer" {
		t.Fatalf("action inattendue : %+v", declaration.Actions[0])
	}
	if err := ValidateWebhooksDeclaration(declaration, "crm"); err != nil {
		t.Fatalf("déclaration chargée refusée : %v", err)
	}
}

func TestLoadWebhooksDeclarationRejectsBrokenJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, config.WebhooksFileName)
	if err := os.WriteFile(path, []byte(`{"schemaVersion": `), 0o644); err != nil {
		t.Fatalf("écriture du test : %v", err)
	}
	if _, err := LoadWebhooksDeclaration(dir); err == nil {
		t.Fatal("JSON cassé accepté")
	}
}

func itoaForTest(i int) string {
	if i == 0 {
		return "0"
	}
	var digits []byte
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}
