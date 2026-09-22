package cmd

import (
	"testing"

	"github.com/protorians/lior-cli/internal/store"
)

func TestNormalizeChannel(t *testing.T) {
	cases := map[string]string{
		"alpha":   "ALPHA",
		" Beta ":  "BETA",
		"release": "RELEASE",
		"RC":      "RC",
	}
	for input, want := range cases {
		got, err := normalizeChannel(input)
		if err != nil {
			t.Fatalf("normalizeChannel(%q): %v", input, err)
		}
		if got != want {
			t.Errorf("normalizeChannel(%q) = %q, want %q", input, got, want)
		}
	}
	if _, err := normalizeChannel("canary"); err == nil {
		t.Error("normalizeChannel doit rejeter un canal inconnu")
	}
}

func TestBuildChannelsAreCanonical(t *testing.T) {
	want := []string{"ALPHA", "BETA", "NIGHTLY", "RC", "RELEASE"}
	if len(buildChannels) != len(want) {
		t.Fatalf("buildChannels = %v, want %v", buildChannels, want)
	}
	for i, c := range want {
		if buildChannels[i] != c {
			t.Errorf("buildChannels[%d] = %q, want %q", i, buildChannels[i], c)
		}
	}
}

func TestSlugifyReusedHelper(t *testing.T) {
	cases := map[string]string{
		"Mon guide d'usage":  "mon-guide-d-usage",
		"  API   Reference ": "api-reference",
		"Été 2026":           "t-2026",
		"":                   "",
	}
	for input, want := range cases {
		if got := slugify(input); got != want {
			t.Errorf("slugify(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestPlatformRows(t *testing.T) {
	rows := platformRows([]store.PlatformSupport{
		{Platform: "WEB", Supported: true, Modes: []string{"SSR", "CSR"}, Os: []string{"macOS"}},
		{Platform: "DESKTOP", Supported: false},
		{Platform: "MOBILE", Supported: true, Modes: []string{"NATIVE"}},
	})
	if len(rows) != 3 {
		t.Fatalf("attendu 3 lignes, reçu %d", len(rows))
	}
	if rows[0][1] != "SSR, CSR · macOS" {
		t.Errorf("ligne web = %q", rows[0][1])
	}
	if rows[1][1] != "non supporté" {
		t.Errorf("ligne desktop = %q", rows[1][1])
	}
	if rows[2][1] != "NATIVE" {
		t.Errorf("ligne mobile = %q", rows[2][1])
	}
}

func TestFindVariable(t *testing.T) {
	variables := []store.EnvironmentVariable{
		{ID: "v1", Key: "API_URL"},
		{ID: "v2", Key: "API_KEY"},
	}
	if got := findVariable(variables, "api_key"); got == nil || got.ID != "v2" {
		t.Errorf("findVariable insensible à la casse: %+v", got)
	}
	if got := findVariable(variables, "MISSING"); got != nil {
		t.Errorf("findVariable doit renvoyer nil, reçu %+v", got)
	}
}

func TestWorkflowLabel(t *testing.T) {
	workflows := []store.Workflow{{ID: "wf-1", Name: "CI"}, {ID: "wf-2", Name: "Release"}}
	if got := workflowLabel(workflows, "wf-2"); got != "Release" {
		t.Errorf("workflowLabel = %q, want Release", got)
	}
	if got := workflowLabel(workflows, "wf-9"); got != "wf-9" {
		t.Errorf("workflowLabel inconnu = %q, want wf-9", got)
	}
}

func TestUpperAll(t *testing.T) {
	got := upperAll([]string{" alpha ", "beta", "", "release"})
	want := []string{"ALPHA", "BETA", "RELEASE"}
	if len(got) != len(want) {
		t.Fatalf("upperAll = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("upperAll[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if got := upperAll(nil); got != nil {
		t.Errorf("upperAll(nil) = %v, want nil", got)
	}
}

func TestArticleTitle(t *testing.T) {
	articles := []store.KnowledgeArticle{{ID: "a1", Title: "Guide"}, {ID: "a2", Title: "Ref"}}
	if got := articleTitle(articles, "a2"); got != "Ref" {
		t.Errorf("articleTitle = %q, want Ref", got)
	}
	if got := articleTitle(articles, "missing"); got != "missing" {
		t.Errorf("articleTitle inconnu = %q, want missing", got)
	}
}

func TestRequirementLabel(t *testing.T) {
	requirements := []store.ModuleRequirement{{ID: "r1", Name: "CRM", ModuleID: "core.crm"}}
	if got := requirementLabel(requirements, "r1"); got != "CRM (core.crm)" {
		t.Errorf("requirementLabel = %q", got)
	}
	if got := requirementLabel(requirements, "r9"); got != "r9" {
		t.Errorf("requirementLabel inconnu = %q, want r9", got)
	}
}

func TestModuleCommandTree(t *testing.T) {
	want := map[string][]string{
		"knowledge":      {"add", "delete", "list", "publish"},
		"workflow":       {"add", "delete", "list", "run", "update"},
		"dev-builds":     {"add", "delete", "list"},
		"channels":       {"list", "pause", "publish", "rollback"},
		"fingerprints":   {"add", "delete", "list"},
		"caches":         {"delete", "list", "purge"},
		"platforms":      {"set"},
		"requirements":   {"add", "delete", "list"},
		"signing-keys":   {"create", "delete", "rotate"},
		"accreditations": {"add", "delete", "list"},
		"variables":      {"list", "set", "unset"},
		"github":         {"link", "unlink"},
	}
	for name, subs := range want {
		var found bool
		for _, c := range moduleCmd.Commands() {
			if c.Name() != name {
				continue
			}
			found = true
			got := map[string]bool{}
			for _, sc := range c.Commands() {
				got[sc.Name()] = true
			}
			for _, sub := range subs {
				if !got[sub] {
					t.Errorf("module %s: sous-commande %q manquante", name, sub)
				}
			}
		}
		if !found {
			t.Errorf("module: commande %q manquante", name)
		}
	}
}
