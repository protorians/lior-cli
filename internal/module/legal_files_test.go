package module

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeLegalFile écrit un document légal à `rel` sous `dir`, en créant les
// répertoires intermédiaires (`src/`).
func writeLegalFile(t *testing.T, dir, rel, body string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func legalFileBody(kind string) string {
	return `{"key":"` + strings.ToLower(kind) + `","kind":"` + kind + `","version":"1.0.0",` +
		`"content":{"Article 1":"Le contrat."}}`
}

func TestCollectLegalDocumentsFromFilesReadsBothDocuments(t *testing.T) {
	dir := t.TempDir()
	writeLegalFile(t, dir, ModuleTermsFileName, legalFileBody(LegalKindTerms))
	writeLegalFile(t, dir, ModulePrivacyFileName, legalFileBody(LegalKindPrivacy))

	documents, errs := CollectLegalDocumentsFromFiles(dir)
	if len(errs) > 0 {
		t.Fatalf("CollectLegalDocumentsFromFiles = %v, want aucune erreur", errs)
	}
	if len(documents) != 2 {
		t.Fatalf("documents = %d, want 2", len(documents))
	}
	if err := ValidateLegalDocuments(documents); err != nil {
		t.Errorf("ValidateLegalDocuments(documents) = %v, want nil", err)
	}
}

func TestLegalDocumentPathsFindsSourceLayout(t *testing.T) {
	// Un arbre installé (packé) range les sources sous `src/` ; les documents
	// légaux y sont résolus au même titre qu'à la racine du module.
	dir := t.TempDir()
	writeLegalFile(t, dir, filepath.Join("src", ModuleTermsFileName), legalFileBody(LegalKindTerms))
	writeLegalFile(t, dir, filepath.Join("src", ModulePrivacyFileName), legalFileBody(LegalKindPrivacy))

	paths := LegalDocumentPaths(dir)
	if len(paths) != 2 {
		t.Fatalf("paths = %d, want 2", len(paths))
	}
	if _, ok := paths["terms"]; !ok {
		t.Errorf("legal document `terms` introuvable sous src/, got %v", paths)
	}
}

func TestCollectLegalDocumentsFromFilesRefusesMalformedJSON(t *testing.T) {
	// Un fichier illisible doit produire une erreur — donc bloquer pack/publish
	// — plutôt que d'être ignoré et de laisser le socle refuser la publication.
	dir := t.TempDir()
	writeLegalFile(t, dir, ModuleTermsFileName, "{not json")

	documents, errs := CollectLegalDocumentsFromFiles(dir)
	if len(documents) != 0 {
		t.Errorf("documents = %d, want 0", len(documents))
	}
	if len(errs) == 0 {
		t.Error("CollectLegalDocumentsFromFiles = aucune erreur, want une erreur de lecture")
	}
}

func TestCollectLegalDocumentsFromFilesReportsFieldProblems(t *testing.T) {
	dir := t.TempDir()
	writeLegalFile(t, dir, ModulePrivacyFileName,
		`{"key":"Privacy_CGV","kind":"CGV","version":"1.0","content":{}}`)

	_, errs := CollectLegalDocumentsFromFiles(dir)
	if len(errs) == 0 {
		t.Error("CollectLegalDocumentsFromFiles = aucune erreur, want une erreur de validation")
	}
}
