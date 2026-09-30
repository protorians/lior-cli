package module

// D16 (spec module-isolated-runtime.md §6.11) — validation de `manifest.legal`
// au `pack`.
//
// Ces tests verrouillent le **réparti** entre le CLI et le serveur : le CLI
// refuse à la publication ce que le serveur refuserait à l'exécution, mais ne
//pretend pas normaliser le contenu. Un test qui laisserait passer une
//déclaration que le serveur refuse donnerait au développeur un module qui se
//verrouille au premier ouverture ; un test qui refuserait une notation
//légitime lui ferait réécrire ses CGU pour rien.

import (
	"encoding/json"
	"strings"
	"testing"
)

// legalContent is the body as it appears in the manifest: raw JSON, so that the
// CLI never becomes a second normalizer of the document text.
func legalContent(raw string) json.RawMessage { return json.RawMessage(raw) }

func terms() LegalDocument {
	return LegalDocument{
		Key:     "terms",
		Kind:    LegalKindTerms,
		Version: "1.0.0",
		Content: legalContent(`[{"title":"Article 1","paragraphs":["Le contrat."]}]`),
	}
}

func privacy() LegalDocument {
	return LegalDocument{
		Key:     "privacy",
		Kind:    LegalKindPrivacy,
		Version: "1.0.0",
		Content: legalContent(`{"Données collectées":{"Responsable":"Liorian."}}`),
	}
}

func TestValidateLegalAcceptsTheThreeNotations(t *testing.T) {
	// Réécrire le même CGU d'une notation à l'autre ne doit rien changer au
	// `pack` : le serveur empreinte la forme canonique, pas la forme écrite
	// (§6.11). Refuser une notation ici romprait cette équivalence pour rien.
	for name, raw := range map[string]string{
		"tableau de sections":  `[{"title":"Article 1","paragraphs":["Le contrat."]}]`,
		"objet imbriqué":       `{"Article 1":{"Objet":"Le contrat."}}`,
		"objet à section-cste": `{"Article 1":"Le contrat."}`,
		// Backtick literals : un corps Markdown est du **texte JSON**, donc les
		// guillemets et les échappements `\n` doivent atteindre le validateur
		// tels quels. Un littéral Go `"## Article 1\n\n…"` produirait du JSON
		// invalide et ferait échouer le cas pour une raison étrangère au test.
		"markdown":     `"## Article 1\n\nLe contrat."`,
		"texte simple": `"Le contrat."`,
	} {
		if err := ValidateLegalContent(legalContent(raw)); err != nil {
			t.Errorf("ValidateLegalContent(%s) = %v, want nil", name, err)
		}
	}
}

func TestValidateLegalRejectsContentlessBodies(t *testing.T) {
	// Un document sans texte produirait une empreinte d'un corps vide : la
	// case à cocher serait acceptée sans que personne n'ait rien lu.
	for name, raw := range map[string]string{
		"chaîne vide":             `""`,
		"chaîne blanche":          `"   "`,
		"tableau vide":            `[]`,
		"objet vide":              `{}`,
		"null":                    `null`,
		"section sans titre":      `[{"paragraphs":["orphelin"]}]`,
		"section sans paragraphe": `[{"title":"Article 1"}]`,
		"section vide":            `{"Article 1":""}`,
		"objet de section vide":   `{"Article 1":{}}`,
		"section inexploitable":   `{"Article 1":42}`,
		"nombre":                  `42`,
	} {
		if err := ValidateLegalContent(legalContent(raw)); err == nil {
			t.Errorf("ValidateLegalContent(%s) = nil, want une erreur", name)
		}
	}
}

func TestValidateLegalDocumentsAcceptsAWellFormedDeclaration(t *testing.T) {
	if err := ValidateLegalDocuments(nil); err != nil {
		t.Errorf("ValidateLegalDocuments(nil) = %v, want nil (aucune obligation légale)", err)
	}
	if err := ValidateLegalDocuments([]LegalDocument{}); err != nil {
		t.Errorf("ValidateLegalDocuments([]) = %v, want nil", err)
	}
	if err := ValidateLegalDocuments([]LegalDocument{terms(), privacy()}); err != nil {
		t.Errorf("ValidateLegalDocuments(TERMS+PRIVACY) = %v, want nil", err)
	}
}

func TestValidateLegalDocumentsRequiresTermsAndPrivacyTogether(t *testing.T) {
	// Le cas que le verrou existe pour empêcher : un module qui livre des CGU
	// sans politique de confidentialité, ou l'inverse.
	for name, documents := range map[string][]LegalDocument{
		"PRIVACY seul":   {privacy()},
		"TERMS seul":     {terms()},
		"LICENSE seul":   {{Key: "license", Kind: LegalKindLicense, Version: "1.0.0", Content: legalContent(`{"Objet":"Licence."}`)}},
		"aucun des deux": {{Key: "a", Kind: LegalKindLicense, Version: "1.0.0", Content: legalContent(`{"Objet":"x"}`)}},
	} {
		if err := ValidateLegalDocuments(documents); err == nil {
			t.Errorf("ValidateLegalDocuments(%s) = nil, want une erreur", name)
		}
	}
}

func TestValidateLegalDocumentsAcceptsOptionalLicence(t *testing.T) {
	no := false
	yes := true
	optional := LegalDocument{
		Key: "license", Kind: LegalKindLicense, Version: "1.0.0", Required: &no,
		Content: legalContent(`{"Objet":"Licence perpétuelle."}`),
	}
	if err := ValidateLegalDocuments([]LegalDocument{terms(), privacy(), optional}); err != nil {
		t.Errorf("licence facultative refusée : %v", err)
	}

	mandatory := optional
	mandatory.Required = &yes
	if err := ValidateLegalDocuments([]LegalDocument{terms(), privacy(), mandatory}); err != nil {
		t.Errorf("licence obligatoire refusée : %v", err)
	}

	// `required` absent vaut `true` : c'est le seul défaut acceptable pour une
	// licence, puisque c'est alors le serveur qui décide de tout bloquer.
	bare := LegalDocument{
		Key: "license", Kind: LegalKindLicense, Version: "1.0.0",
		Content: legalContent(`{"Objet":"Licence."}`),
	}
	if !bare.IsRequired() {
		t.Error("un document sans `required` doit être bloquant")
	}
}

func TestValidateLegalDocumentRefusesNonBlockingTermsOrPrivacy(t *testing.T) {
	// Publier des CGU en `required: false` produirait un module dont personne
	// n'est obligé de les lire. Le serveur le refuse ; le `pack` doit le dire
	// au développeur plutôt que de le laisser publier un module cassé.
	no := false
	for _, kind := range []string{LegalKindTerms, LegalKindPrivacy} {
		document := LegalDocument{
			Key: "doc", Kind: kind, Version: "1.0.0", Required: &no,
			Content: legalContent(`{"Article 1":"texte"}`),
		}
		err := ValidateLegalDocument(document)
		if err == nil {
			t.Errorf("%s en required:false accepté, want une erreur", kind)
			continue
		}
		if !strings.Contains(err.Error(), "LICENSE") {
			t.Errorf("%s : l'erreur devrait nommer LICENSE, got %q", kind, err)
		}
	}
}

func TestValidateLegalDocumentRefusesUnidentifiableEntries(t *testing.T) {
	base := terms()
	for name, mutate := range map[string]func(*LegalDocument){
		"clé non kebab":      func(d *LegalDocument) { d.Key = "Terms_Of_Use" },
		"clé absente":        func(d *LegalDocument) { d.Key = "" },
		"catégorie inconnue": func(d *LegalDocument) { d.Kind = "CGV" },
		"catégorie absente":  func(d *LegalDocument) { d.Kind = "" },
		"version absente":    func(d *LegalDocument) { d.Version = "" },
		"version non semver": func(d *LegalDocument) { d.Version = "1.0" },
		"version invalide":   func(d *LegalDocument) { d.Version = "latest" },
	} {
		document := base
		mutate(&document)
		if err := ValidateLegalDocument(document); err == nil {
			t.Errorf("%s accepté, want une erreur", name)
		}
	}
}

func TestValidateLegalDocumentsRefusesDuplicates(t *testing.T) {
	// Un doublon rendrait la liste des cases à cocher ambiguë, et la clé
	// d'acceptation avec elle.
	doubledKey := privacy()
	doubledKey.Key = "terms"
	if err := ValidateLegalDocuments([]LegalDocument{terms(), doubledKey}); err == nil {
		t.Error("clé en double acceptée, want une erreur")
	}

	secondPrivacy := privacy()
	secondPrivacy.Key = "privacy-cgv"
	if err := ValidateLegalDocuments([]LegalDocument{terms(), privacy(), secondPrivacy}); err == nil {
		t.Error("catégorie en double acceptée, want une erreur")
	}
}

// Le contenu est du JSON brut : le round-trip doit être exact, sinon le CLI
// réécrirait les CGU du développeur à chaque `pack`.
func TestLegalContentRoundTripsUnchanged(t *testing.T) {
	raw := `{"Article 1":{"Objet":"Le contrat."},"Article 2":"La suite."}`
	encoded, err := json.Marshal(LegalDocument{
		Key: "terms", Kind: LegalKindTerms, Version: "1.0.0", Content: legalContent(raw),
	})
	if err != nil {
		t.Fatal(err)
	}

	var back LegalDocument
	if err := json.Unmarshal(encoded, &back); err != nil {
		t.Fatal(err)
	}
	if got := string(back.Content); got != raw {
		t.Errorf("contenu altéré par le round-trip :\n  avant %s\n  après %s", raw, got)
	}
}
