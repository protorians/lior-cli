package module

// D16 (spec module-isolated-runtime.md §6.11) — validation of `manifest.legal`
// at pack time.
//
// ## Where the split falls
//
// The CLI checks the **envelope**: that a declared document is identifiable,
// versioned, categorised and non-empty. It deliberately does *not* normalize
// the content or compute its checksum — the server owns both
// (`@liorian/api-resources/module-legal.util`), because the checksum is what
// the acceptance is recorded against, and a second implementation would be free
// to drift from the one that decides.
//
// The set-level rules (TERMS and PRIVACY both present, no duplicate key, no
// duplicate kind) are checked here even though the server also rejects them.
// Refusing a manifest that would be rejected anyway, at the moment the
// developer runs `pack`, is worth more than the duplication: the alternative is
// a module that publishes fine and locks its first user out.

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// legalContentKind names the accepted notations of a legal document body.
type legalContentKind int

const (
	legalContentInvalid legalContentKind = iota
	legalContentMarkdown
	legalContentSections
	legalContentObject
)

// ValidateLegalContent checks that a document body is one of the three
// accepted notations and carries actual text.
//
// The three are deliberately interchangeable: a publisher may rewrite their
// terms from the array notation to Markdown without the acceptance being
// invalidated, because the server hashes the canonical form rather than the
// written one (§6.11). So nothing here rejects a notation, only an absent or
// contentless body.
func ValidateLegalContent(raw json.RawMessage) error {
	kind, err := classifyLegalContent(raw)
	if err != nil {
		return err
	}
	switch kind {
	case legalContentMarkdown, legalContentSections, legalContentObject:
		return nil
	default:
		return errors.New("content is empty")
	}
}

// classifyLegalContent determines which notation a body uses, and rejects the
// shapes that carry no text — an empty object, an array of empty sections, a
// string of whitespace.
func classifyLegalContent(raw json.RawMessage) (legalContentKind, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return legalContentInvalid, errors.New("content is required")
	}

	switch trimmed[0] {
	case '"':
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return legalContentInvalid, fmt.Errorf("content is not readable text: %w", err)
		}
		if strings.TrimSpace(text) == "" {
			return legalContentInvalid, errors.New("content is empty")
		}
		return legalContentMarkdown, nil

	case '[':
		var sections []map[string]any
		if err := json.Unmarshal(raw, &sections); err != nil {
			return legalContentInvalid, fmt.Errorf("content array is not readable: %w", err)
		}
		if len(sections) == 0 {
			return legalContentInvalid, errors.New("content array holds no section")
		}
		for i, section := range sections {
			title, ok := section["title"].(string)
			if !ok || strings.TrimSpace(title) == "" {
				return legalContentInvalid, fmt.Errorf("content section %d has no title", i+1)
			}
			paragraphs, _ := section["paragraphs"].([]any)
			if len(paragraphs) == 0 {
				return legalContentInvalid, fmt.Errorf("content section %d holds no paragraph", i+1)
			}
		}
		return legalContentSections, nil

	case '{':
		var sections map[string]any
		if err := json.Unmarshal(raw, &sections); err != nil {
			return legalContentInvalid, fmt.Errorf("content object is not readable: %w", err)
		}
		if len(sections) == 0 {
			return legalContentInvalid, errors.New("content object holds no section")
		}
		// A section is either a bare string or a `{title: paragraph}` map. Both
		// are accepted; anything else would be silently dropped by the server's
		// normalizer, so it is refused here rather than at first use.
		for name, body := range sections {
			switch paragraph := body.(type) {
			case string:
				if strings.TrimSpace(paragraph) == "" {
					return legalContentInvalid, fmt.Errorf("content section %q is empty", name)
				}
			case map[string]any:
				if len(paragraph) == 0 {
					return legalContentInvalid, fmt.Errorf("content section %q holds no paragraph", name)
				}
			default:
				return legalContentInvalid, fmt.Errorf(
					"content section %q is neither a string nor a {title: paragraph} object", name)
			}
		}
		return legalContentObject, nil

	default:
		return legalContentInvalid, errors.New("content is a string, an array of sections or an object of sections")
	}
}

// ValidateLegalDocument checks one entry of `manifest.legal`.
func ValidateLegalDocument(document LegalDocument) error {
	if !kebabNameRE.MatchString(strings.TrimSpace(document.Key)) {
		return fmt.Errorf("key %q is not kebab-case", document.Key)
	}

	switch strings.ToUpper(strings.TrimSpace(document.Kind)) {
	case LegalKindTerms, LegalKindPrivacy, LegalKindLicense:
	default:
		return fmt.Errorf(
			"kind %q is not one of %s, %s, %s",
			document.Kind, LegalKindTerms, LegalKindPrivacy, LegalKindLicense)
	}

	// A document revision is mandatory even though it is not the module
	// version: without it, two revisions of the same terms would be
	// indistinguishable and accepting one would accept the other.
	if !isSemver(document.Version) {
		return fmt.Errorf("version %q is not SemVer", document.Version)
	}

	// Only LICENSE may be non-blocking. A terms or privacy document declared
	// `required: false` would publish a module nobody is obliged to read before
	// using it — the server refuses it, so the pack does too.
	if !document.IsRequired() && !strings.EqualFold(document.Kind, LegalKindLicense) {
		return fmt.Errorf("required: false is admitted for %s only, not for %s", LegalKindLicense, document.Kind)
	}

	return ValidateLegalContent(document.Content)
}

// ValidateLegalDocuments checks the whole `legal` declaration: each document,
// then the set-level rules that no single entry can express.
//
// An empty or absent declaration is valid and means "no legal obligation, no
// gate" — that is the state of every existing module, and it must stay cheap.
func ValidateLegalDocuments(documents []LegalDocument) error {
	if len(documents) == 0 {
		return nil
	}

	keys := make(map[string]bool, len(documents))
	kinds := make(map[string]bool, len(documents))
	for _, document := range documents {
		if err := ValidateLegalDocument(document); err != nil {
			return err
		}

		key := strings.ToLower(strings.TrimSpace(document.Key))
		if keys[key] {
			return fmt.Errorf("duplicate key %q in legal", document.Key)
		}
		keys[key] = true

		// One document per category. Two TERMS entries would make the checkbox
		// list ambiguous and the acceptance key ambiguous with it.
		kind := strings.ToUpper(strings.TrimSpace(document.Kind))
		if kinds[kind] {
			return fmt.Errorf("duplicate %s document in legal", kind)
		}
		kinds[kind] = true
	}

	// Declaring any document means declaring both. The alternative — a module
	// shipping terms without a privacy policy — is the exact case the gate
	// exists to prevent, and it must be unpublishable rather than merely
	// discouraged.
	if !kinds[LegalKindTerms] || !kinds[LegalKindPrivacy] {
		return fmt.Errorf("a non-empty legal declaration requires both %s and %s", LegalKindTerms, LegalKindPrivacy)
	}

	return nil
}
