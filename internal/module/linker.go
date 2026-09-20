package module

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/protorians/lior-cli/internal/config"
	"github.com/protorians/lior-cli/internal/pkg"
)

// linksFileName is the project-local state recording module ↔ remote store
// associations. It makes `LinkedModules` independent from the remote token
// string format (UUID ⇒ local was fragile).
const linksFileName = "links.json"

// RemoteInfo carries the remote store metadata to merge into a local manifest
// when linking (spec §5.7 step 6: fill in absent remote metadata).
type RemoteInfo struct {
	Token         string
	Name          string
	Description   string
	Version       string
	PublisherID   string
	PublisherName string
}

// LinkResult summarises a link operation.
type LinkResult struct {
	LocalModule   string
	RemoteToken   string
	RemoteName    string
	RemoteVersion string
}

// Linker manages the link between local modules and remote store modules.
type Linker struct {
	Root string
}

// Link associates a local module with a remote store module. The remote
// metadata is merged into the local manifest for fields that are still empty.
func (l *Linker) Link(name string, remote RemoteInfo) (*LinkResult, error) {
	moduleDir := filepath.Join(l.Root, config.ExternalModulesDir, name)
	if !pkg.DirExists(moduleDir) {
		return nil, fmt.Errorf("module %q does not exist in %s", name, config.ExternalModulesDir)
	}

	manifestPath := filepath.Join(moduleDir, config.ManifestFileName)
	m, err := LoadManifest(manifestPath)
	if err != nil {
		return nil, err
	}

	// Merge remote metadata into the locally absent fields (spec §5.7 step 6).
	m.Token = remote.Token
	if m.Name == "" {
		m.Name = remote.Name
	}
	if m.Description == "" {
		m.Description = remote.Description
	}
	if m.Publisher.ID == "" {
		m.Publisher.ID = remote.PublisherID
	}
	if m.Publisher.Name == "" {
		m.Publisher.Name = remote.PublisherName
	}
	if err := m.Save(manifestPath); err != nil {
		return nil, fmt.Errorf("failed to update manifest: %w", err)
	}

	// Record the association so LinkedModules does not depend on the token
	// string format.
	state := l.loadLinks()
	state.Modules[name] = remote.Token
	if err := l.saveLinks(state); err != nil {
		return nil, fmt.Errorf("failed to record the link: %w", err)
	}

	remoteName := remote.Name
	if remoteName == "" {
		remoteName = m.Name
	}
	remoteVersion := remote.Version
	if remoteVersion == "" {
		remoteVersion = m.Version
	}

	return &LinkResult{
		LocalModule:   name,
		RemoteToken:   remote.Token,
		RemoteName:    remoteName,
		RemoteVersion: remoteVersion,
	}, nil
}

// Unlink removes the remote store association from a local module.
func (l *Linker) Unlink(name string) error {
	moduleDir := filepath.Join(l.Root, config.ExternalModulesDir, name)
	if !pkg.DirExists(moduleDir) {
		return fmt.Errorf("module %q does not exist in %s", name, config.ExternalModulesDir)
	}

	manifestPath := filepath.Join(moduleDir, config.ManifestFileName)
	m, err := LoadManifest(manifestPath)
	if err != nil {
		return err
	}

	// Generate a new local UUID token (de-link from remote).
	m.Token = pkg.NewUUID()
	if err := m.Save(manifestPath); err != nil {
		return fmt.Errorf("failed to update manifest: %w", err)
	}

	state := l.loadLinks()
	delete(state.Modules, name)
	if err := l.saveLinks(state); err != nil {
		return fmt.Errorf("failed to record the unlink: %w", err)
	}

	return nil
}

// LinkedModules returns the modules currently linked to a remote store module.
// The links state file is authoritative; manifests carrying a non-UUID token
// are merged in as a migration bridge for projects linked before this state
// was introduced.
func (l *Linker) LinkedModules() ([]string, error) {
	dir := filepath.Join(l.Root, config.ExternalModulesDir)
	if !pkg.DirExists(dir) {
		return nil, fmt.Errorf("directory %q not found", config.ExternalModulesDir)
	}

	linked := map[string]bool{}
	state := l.loadLinks()
	for name, token := range state.Modules {
		if token == "" || !pkg.DirExists(filepath.Join(dir, name)) {
			continue
		}
		linked[name] = true
	}

	entries, err := filepath.Glob(filepath.Join(dir, "*", config.ManifestFileName))
	if err != nil {
		return nil, fmt.Errorf("failed to search manifests: %w", err)
	}
	for _, manifestPath := range entries {
		moduleName := filepath.Base(filepath.Dir(manifestPath))
		if linked[moduleName] {
			continue
		}
		m, err := LoadManifest(manifestPath)
		if err != nil {
			continue
		}
		if m.Token == "" || pkg.IsUUID(m.Token) {
			continue // locally generated token, not linked to a remote store module
		}
		linked[moduleName] = true
	}

	names := make([]string, 0, len(linked))
	for name := range linked {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// RemoteToken returns the remote store token associated with a local module
// through the links state ("" when not linked).
func (l *Linker) RemoteToken(name string) string {
	if name == "" {
		return ""
	}
	state := l.loadLinks()
	return state.Modules[name]
}

// linksPath returns the project-local links state file path.
func (l *Linker) linksPath() string {
	return filepath.Join(l.Root, config.LorianDir, linksFileName)
}

// linksState maps local module names to remote store tokens.
type linksState struct {
	Modules map[string]string `json:"modules"`
}

// loadLinks reads the links state file, tolerating absence.
func (l *Linker) loadLinks() linksState {
	var state linksState
	data, err := os.ReadFile(l.linksPath())
	if err == nil {
		if json.Unmarshal(data, &state) != nil {
			state = linksState{}
		}
	}
	if state.Modules == nil {
		state.Modules = map[string]string{}
	}
	return state
}

// saveLinks writes the links state file,  if needed.
func (l *Linker) saveLinks(state linksState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return pkg.WriteFile(l.linksPath(), data)
}
