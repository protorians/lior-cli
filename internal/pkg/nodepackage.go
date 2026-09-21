package pkg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// NodePackage is the minimal view of a `package.json` the CLI needs: the
// runtime and development dependency maps. Since the module manifest no longer
// carries `dependencies`/`devDependencies`, the `package.json` of the module
// (or of the project) is the single source of truth for npm packages.
type NodePackage struct {
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
}

// LoadNodePackage reads and decodes a `package.json` file. A missing or
// malformed file yields an empty package (never nil) so callers can treat "no
// declared dependencies" uniformly.
func LoadNodePackage(path string) *NodePackage {
	np := &NodePackage{}
	data, err := os.ReadFile(path)
	if err != nil {
		return np
	}
	if err := json.Unmarshal(data, np); err != nil {
		return &NodePackage{}
	}
	return np
}

// DependencyNames returns the names of every declared dependency (runtime then
// development), sorted and deduplicated.
func (p *NodePackage) DependencyNames() []string {
	seen := map[string]bool{}
	for name := range p.Dependencies {
		seen[name] = true
	}
	for name := range p.DevDependencies {
		seen[name] = true
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// RuntimeDependencyNames returns the sorted names of the runtime dependencies
// only (the ones the audit checks for installation).
func (p *NodePackage) RuntimeDependencyNames() []string {
	names := make([]string, 0, len(p.Dependencies))
	for name := range p.Dependencies {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// LatestDependencies returns the sorted names of the dependencies declared with
// the explicit `latest` specifier. Package managers frequently ignore such a
// specifier during a plain install (lockfile or already-installed package), so
// the CLI re-installs them explicitly.
func (p *NodePackage) LatestDependencies() []string {
	var names []string
	for name, spec := range p.Dependencies {
		if isLatestSpec(spec) {
			names = append(names, name)
		}
	}
	for name, spec := range p.DevDependencies {
		if isLatestSpec(spec) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// isLatestSpec reports whether a dependency specifier is the explicit `latest`
// tag (case-insensitive, surrounding spaces tolerated).
func isLatestSpec(spec string) bool {
	return strings.EqualFold(strings.TrimSpace(spec), "latest")
}

// ForceInstallLatest explicitly (re)installs the dependencies declared with the
// `latest` specifier in dir's `package.json`, using the package manager `pm`.
// A plain install often leaves `latest` unresolved, so each one is added
// explicitly with `@latest`. It returns the forced package names, or nil when
// there is nothing to do.
func ForceInstallLatest(dir, pm string) ([]string, error) {
	latest := LoadNodePackage(filepath.Join(dir, "package.json")).LatestDependencies()
	if len(latest) == 0 || pm == "" {
		return nil, nil
	}
	specs := make([]string, len(latest))
	for i, name := range latest {
		specs[i] = name + "@latest"
	}
	args := DependencyArgs(pm, specs...)
	if args == nil {
		return nil, nil
	}
	if err := StreamCommandIn(dir, pm, args...); err != nil {
		return latest, err
	}
	return latest, nil
}
