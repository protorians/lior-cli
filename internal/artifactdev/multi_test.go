package artifactdev

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeModule écrit un module minimal (entrée, manifeste, template hôte) et
// renvoie sa racine — see artifactdev_test.go for the shared helper.
func writeMultiModule(t *testing.T, id string) string {
	t.Helper()
	moduleDir := writeModule(t)
	manifest := `{"id":"` + id + `","name":"` + id + `","version":"1.0.0","entry":"main.tsx"}`
	if err := os.WriteFile(filepath.Join(moduleDir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return moduleDir
}

func TestSlugifyIdentifierMatchesSocleConvention(t *testing.T) {
	cases := map[string]string{
		"crm":                  "crm",
		"mod.liorian.crm":      "mod-liorian-crm",
		"mod.liorian.test-1":   "mod-liorian-test-1",
		"Hello World":          "hello-world",
		"  --trait-- d'union ": "trait-d-union",
	}
	for input, want := range cases {
		if got := slugifyIdentifier(input); got != want {
			t.Fatalf("slugifyIdentifier(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestResolveTenantsRegistersSlugAndAliases(t *testing.T) {
	dir := writeMultiModule(t, "mod.liorian.crm")
	tenants, registry, err := resolveTenants([]string{dir}, DevOptions{}, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if len(tenants) != 1 || tenants[0].identifier != "mod.liorian.crm" {
		t.Fatalf("tenants = %+v", tenants)
	}
	if tenants[0].slug != "mod-liorian-crm" {
		t.Fatalf("slug = %q", tenants[0].slug)
	}
	// Le slug canonique ET le slug du nom de dossier sont servis.
	folderAlias := slugifyIdentifier(filepath.Base(dir))
	if registry["mod-liorian-crm"] != tenants[0] || registry[folderAlias] != tenants[0] {
		t.Fatalf("registry = %+v (folder alias %q)", registry, folderAlias)
	}
}

func TestResolveTenantsRejectsSlugCollision(t *testing.T) {
	// Deux dossiers distincts qui revendiquent le même identifiant : la
	// deuxième inscription doit échouer plutôt que servir le mauvais module.
	first := writeMultiModule(t, "crm")
	second := writeMultiModule(t, "crm")
	if _, _, err := resolveTenants([]string{first, second}, DevOptions{}, func(string) {}); err == nil {
		t.Fatal("expected collision error")
	}
}

func TestResolveTenantsDeduplicatesSameDirectory(t *testing.T) {
	dir := writeMultiModule(t, "crm")
	tenants, _, err := resolveTenants([]string{dir, dir}, DevOptions{}, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if len(tenants) != 1 {
		t.Fatalf("tenants = %d, want 1", len(tenants))
	}
}

func TestMultiHandlerServesEachTenantUnderItsSlug(t *testing.T) {
	dirA := writeMultiModule(t, "mod.liorian.crm")
	dirB := writeMultiModule(t, "mod.liorian.billing")
	tenants, registry, err := resolveTenants([]string{dirA, dirB}, DevOptions{}, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	// Contenu d'artefact distinct par tenant, pour prouver le cloisonnement.
	for i, tenant := range tenants {
		if err := os.MkdirAll(tenant.cfg.ArtifactDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(tenant.cfg.ArtifactDir, "index.html"),
			[]byte("<html><body>"+tenant.identifier+"</body></html>"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(tenant.cfg.ArtifactDir, tenant.cfg.Bundle),
			[]byte("// "+tenant.identifier), 0o644); err != nil {
			t.Fatal(err)
		}
		_ = i
	}

	handler := newMultiHandler(registry, newBroadcaster())

	// Le document hôte de chaque slug est servi avec le reload taggué du
	// tenant — crm ne reçoit jamais le script de billing.
	for _, tenant := range tenants {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/"+tenant.slug+"/", nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("GET /%s/ -> %d", tenant.slug, recorder.Code)
		}
		body := recorder.Body.String()
		if !strings.Contains(body, tenant.identifier) {
			t.Fatalf("GET /%s/ served another module: %s", tenant.slug, body)
		}
		if !strings.Contains(body, "reload:"+tenant.slug) {
			t.Fatalf("GET /%s/ missing tagged reload: %s", tenant.slug, body)
		}
	}

	// L'alias dossier sert aussi (dossier temporaire → slug de son nom).
	folderAlias := slugifyIdentifier(filepath.Base(dirA))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/"+folderAlias+"/", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "mod.liorian.crm") {
		t.Fatalf("folder alias broken: %d %s", recorder.Code, recorder.Body.String())
	}

	// Un chemin inconnu ne tombe jamais sur le document d'un autre tenant.
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/unknown/", nil))
	if recorder.Code != http.StatusOK || strings.Contains(recorder.Body.String(), "mod.liorian") {
		t.Fatalf("root page should be the module index: %d %s", recorder.Code, recorder.Body.String())
	}

	// Le bundle est servi tel quel sous le slug.
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/crm/module.js", nil))
	// L'alias `crm` n'existe que si aucun autre tenant ne le détient : ici
	// billing est détenteur, la requête retombe sur l'annuaire — on passe par
	// le slug canonique pour la vérification du bundle.
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/mod-liorian-crm/module.js", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "mod.liorian.crm") {
		t.Fatalf("bundle serving broken: %d %s", recorder.Code, recorder.Body.String())
	}
}
