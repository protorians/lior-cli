package module

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/pkg"
)

// ModuleLegalFileNames are the optional legal document files a module may carry
// at its root (D16, spec module-isolated-runtime.md §6.11). The packer embeds
// them under `src/`; they are never referenced from the signed manifest.
const (
	ModuleTermsFileName   = "module.terms.json"
	ModulePrivacyFileName = "module.privacy.json"
)

// LegalDocumentFile is the parsed content of a legal document file
// (module.terms.json or module.privacy.json).
type LegalDocumentFile struct {
	Key      string          `json:"key"`
	Kind     string          `json:"kind"`
	Title    string          `json:"title,omitempty"`
	Version  string          `json:"version"`
	Required *bool           `json:"required,omitempty"`
	Content  json.RawMessage `json:"content"`
}

// IsRequired reports whether the document blocks module use until accepted.
func (d LegalDocumentFile) IsRequired() bool { return d.Required == nil || *d.Required }

// LegalDocumentPaths resolves the legal document files of a module directory,
// trying the workspace source layout (module root) first, then the
// distribution layout (`src/`, where the packer stores the sources). It returns
// a map of kind to path, empty when the module carries no such files.
func LegalDocumentPaths(moduleDir string) map[string]string {
	paths := make(map[string]string)
	for _, rel := range []string{
		ModuleTermsFileName, filepath.Join("src", ModuleTermsFileName),
		ModulePrivacyFileName, filepath.Join("src", ModulePrivacyFileName),
	} {
		p := filepath.Join(moduleDir, rel)
		if pkg.FileExists(p) {
			kind := strings.TrimSuffix(filepath.Base(rel), ".json")
			kind = strings.TrimPrefix(kind, "module.")
			paths[kind] = p
		}
	}
	return paths
}

// LoadLegalDocumentFile reads and parses a legal document file. A malformed
// document (invalid JSON, or a value of the wrong type) is refused.
func LoadLegalDocumentFile(path string) (*LegalDocumentFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New(i18n.Tf("module.legal.error.unreadable", err.Error()))
	}
	var file LegalDocumentFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, errors.New(i18n.Tf("module.legal.error.unreadable", err.Error()))
	}
	return &file, nil
}

// ValidateLegalDocumentFile applies the envelope validation of D16 §6.11:
// key (kebab-case), kind (TERMS/PRIVACY/LICENSE), version (SemVer), required
// (only LICENSE may be false), and content (non-empty, one of three notations).
// It returns the list of problems (empty when the file is valid).
func ValidateLegalDocumentFile(file *LegalDocumentFile, kind string) []string {
	if file == nil {
		return []string{i18n.T("module.legal.error.object")}
	}
	var errs []string
	if !kebabNameRE.MatchString(strings.TrimSpace(file.Key)) {
		errs = append(errs, i18n.Tf("module.legal.error.key", kind, file.Key))
	}
	switch strings.ToUpper(strings.TrimSpace(file.Kind)) {
	case LegalKindTerms, LegalKindPrivacy, LegalKindLicense:
	default:
		errs = append(errs, i18n.Tf("module.legal.error.kind", kind, file.Kind))
	}
	if !isSemver(file.Version) {
		errs = append(errs, i18n.Tf("module.legal.error.version", kind, file.Version))
	}
	if !file.IsRequired() && !strings.EqualFold(file.Kind, LegalKindLicense) {
		errs = append(errs, i18n.Tf("module.legal.error.required", kind, LegalKindLicense))
	}
	if err := ValidateLegalContent(file.Content); err != nil {
		errs = append(errs, i18n.Tf("module.legal.error.content", kind, err.Error()))
	}
	return errs
}

// CollectLegalDocumentsFromFiles loads and validates all legal document files
// in a module directory. It returns the combined list of documents and any
// validation errors.
func CollectLegalDocumentsFromFiles(moduleDir string) ([]LegalDocument, []string) {
	var documents []LegalDocument
	var allErrs []string

	paths := LegalDocumentPaths(moduleDir)
	for kind, path := range paths {
		file, err := LoadLegalDocumentFile(path)
		if err != nil {
			allErrs = append(allErrs, i18n.Tf("module.legal.error.load", kind, err.Error()))
			continue
		}
		if errs := ValidateLegalDocumentFile(file, kind); len(errs) > 0 {
			allErrs = append(allErrs, errs...)
			continue
		}
		documents = append(documents, LegalDocument{
			Key:      file.Key,
			Kind:     strings.ToUpper(strings.TrimSpace(file.Kind)),
			Title:    file.Title,
			Version:  file.Version,
			Required: file.Required,
			Content:  file.Content,
		})
	}

	// No separate legal files: the caller falls back to `manifest.legal`, kept
	// for backward compatibility during the migration to files (§6.11).

	return documents, allErrs
}
