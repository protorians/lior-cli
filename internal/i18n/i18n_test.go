package i18n

import (
	"strings"
	"testing"
)

func TestAvailableLanguages(t *testing.T) {
	langs := AvailableLanguages()
	found := map[string]bool{}
	for _, l := range langs {
		found[l] = true
	}
	if !found["en-US"] {
		t.Error("en-US catalog must be embedded")
	}
	if !found["fr-FR"] {
		t.Error("fr-FR catalog must be embedded")
	}
}

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"":            DefaultLang,
		"en":          "en-US",
		"en-US":       "en-US",
		"en_US":       "en-US",
		"EN-EN":       "en-US",
		"en-GB":       "en-US",
		"fr":          "fr-FR",
		"fr-FR":       "fr-FR",
		"fr_FR":       "fr-FR",
		"FR":          "fr-FR",
		"fr-FR.UTF-8": "fr-FR",
		"fr_FR.UTF-8": "fr-FR",
		"sr-Cyrl-RS":  DefaultLang,
		"zz_ZZ":       DefaultLang,
		"   fr-FR  ":  "fr-FR",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUseSwitchesCatalog(t *testing.T) {
	old := Language()
	defer Use(old)

	if got := Use("fr"); got != "fr-FR" {
		t.Fatalf("Use(fr) = %q, want fr-FR", got)
	}
	if got := Language(); got != "fr-FR" {
		t.Fatalf("Language() = %q, want fr-FR", got)
	}
	if got := T("cmd.init.short"); got == "" || got == "cmd.init.short" {
		t.Fatalf("fr translation missing: %q", got)
	}

	// Unknown language falls back to the default catalog.
	if got := Use("de-DE"); got != DefaultLang {
		t.Errorf("Use(de-DE) = %q, want %q", got, DefaultLang)
	}
	if got := T("init.success"); !strings.Contains(got, "initialized") {
		t.Errorf("fallback should return en-US text, got %q", got)
	}
}

func TestTranslationDiffersByLanguage(t *testing.T) {
	old := Language()
	defer Use(old)

	en := Use("en")
	enInit := T("init.success")
	enSystem := Use("fr")
	frInit := T("init.success")
	if enInit == frInit {
		t.Errorf("translation identical for en/fr: %q", enInit)
	}
	_ = enSystem
	_ = en
}

func TestMissingKeyFallsBackToKey(t *testing.T) {
	old := Language()
	defer Use(old)

	Use("fr")
	if got := T("no.such.key"); got != "no.such.key" {
		t.Errorf("missing key = %q, want key itself", got)
	}
}

func TestTfFormatsArguments(t *testing.T) {
	old := Language()
	defer Use(old)

	Use("en-US")
	if got := Tf("modules.error.module_absent", "blog", "library/modules"); !strings.Contains(got, "blog") || !strings.Contains(got, "library/modules") {
		t.Errorf("Tf formatting failed: %q", got)
	}
}

func TestCatalogKeysMatchBetweenLanguages(t *testing.T) {
	en := catalogs["en-US"]
	fr := catalogs["fr-FR"]
	if en == nil || fr == nil {
		t.Fatal("catalogs not loaded")
	}
	for key := range en {
		if _, ok := fr[key]; !ok {
			t.Errorf("key %q is missing from fr-FR catalog", key)
		}
	}
	for key := range fr {
		if _, ok := en[key]; !ok {
			t.Errorf("key %q is missing from en-US catalog", key)
		}
	}
}

func TestVerbParityAcrossCatalogs(t *testing.T) {
	en := catalogs["en-US"]
	fr := catalogs["fr-FR"]
	for key, enVal := range en {
		frVal := fr[key]
		if verbCount(enVal) != verbCount(frVal) {
			t.Errorf("verb count mismatch for %q: en=%s fr=%s", key, enVal, frVal)
		}
	}
}

// verbCount reports the number of printf verbs (%s, %d, %q, %v, …) in a string.
func verbCount(s string) int {
	n := 0
	for i := 0; i < len(s)-1; i++ {
		if s[i] == '%' {
			if s[i+1] == '%' {
				i++
				continue
			}
			n++
		}
	}
	return n
}
