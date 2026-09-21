package cmd

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/pkg"
	"github.com/protorians/lior-cli/internal/tui"
)

// envAutoEnv forces auto-configuration of the .env (same effect as --auto-env)
// in non-interactive or scripted runs.
const envAutoEnv = "LIORIAN_CLI_ENV_AUTO"

// envAutoConfigured reports whether the .env must be filled with the suggested
// values without prompting, either because --auto-env was passed or the
// LIORIAN_CLI_ENV_AUTO environment variable is set.
func envAutoConfigured() bool {
	return initAutoEnv || os.Getenv(envAutoEnv) != ""
}

// envPlan is a parsed and synthesised .env, ready to be completed (interactive
// prompting) and written to disk.
type envPlan struct {
	path string
	file *pkg.EnvFile
}

// envGenerationNeeded reports whether targetDir has a .env sample to expand and
// no existing .env to preserve.
func envGenerationNeeded(targetDir string) bool {
	if pkg.FileExists(filepath.Join(targetDir, pkg.EnvFileName)) {
		debugf("%s already exists, keeping it untouched", pkg.EnvFileName)
		return false
	}
	sample := pkg.FindEnvSample(targetDir)
	if sample == "" {
		debugf("no .env sample found in %s, skipping", targetDir)
		return false
	}
	return true
}

// planEnvFromSample reads the template's .env sample (see
// pkg.EnvSampleCandidates) and fills the known variables with generated or
// project-derived values (application key, project name/slug, VAPID key pair).
// completeEnv then offers each configurable value as an editable placeholder.
func planEnvFromSample(targetDir, projectName string) (*envPlan, error) {
	samplePath := pkg.FindEnvSample(targetDir)
	if samplePath == "" {
		return nil, nil
	}
	data, err := os.ReadFile(samplePath)
	if err != nil {
		return nil, pkg.NewError(i18n.T("cat.project"), i18n.Tf("init.error.env_read", samplePath, err.Error()), pkg.ExitError)
	}

	env := pkg.ParseEnv(data)
	if err := synthesizeEnv(env, projectName); err != nil {
		return nil, pkg.NewError(i18n.T("cat.project"), i18n.Tf("init.error.env", err.Error()), pkg.ExitError)
	}
	return &envPlan{path: filepath.Join(targetDir, pkg.EnvFileName), file: env}, nil
}

// completeEnv prompts for every configurable variable unless auto is set, then
// writes the .env to disk with user-only permissions. Auto-configuration keeps
// the synthesised/default values as-is.
func (p *envPlan) completeEnv(auto bool) error {
	if !auto {
		if err := promptEnvValues(p.file); err != nil {
			return err
		}
	}
	if err := os.WriteFile(p.path, []byte(p.file.Render()), 0o600); err != nil {
		return pkg.NewError(i18n.T("cat.project"), i18n.Tf("init.error.env_write", p.path, err.Error()), pkg.ExitError)
	}
	return nil
}

// generateEnvFromSample is the synchronous entry point: it plans and completes
// the .env in one call. It returns whether a .env was created.
func generateEnvFromSample(targetDir, projectName string) (bool, error) {
	if !envGenerationNeeded(targetDir) {
		return false, nil
	}
	plan, err := planEnvFromSample(targetDir, projectName)
	if err != nil || plan == nil {
		return false, err
	}
	if err := plan.completeEnv(false); err != nil {
		return false, err
	}
	return true, nil
}

// synthesizeEnv fills known variables with generated or project-derived values.
func synthesizeEnv(env *pkg.EnvFile, projectName string) error {
	for _, key := range env.Keys() {
		upper := strings.ToUpper(key)
		switch {
		case upper == "APP_KEY" || strings.HasSuffix(upper, "ENCRYPTION_KEY"):
			value, err := pkg.NewEncryptionKey()
			if err != nil {
				return err
			}
			env.Set(key, value)
		case strings.HasSuffix(upper, "APP_NAME"):
			env.Set(key, projectName)
		case strings.HasSuffix(upper, "APP_SLUG"):
			env.Set(key, slugify(projectName))
		case strings.HasSuffix(upper, "VAPID_PUBLIC_KEY"):
			public, private, err := pkg.NewVAPIDKeys()
			if err != nil {
				return err
			}
			env.Set(key, public)
			env.Set(vapidPrivateKey(key), private)
		}
	}
	return nil
}

// envKeyLabel renders a human-friendly label for an env key: the NEXT_PUBLIC_
// prefix is dropped and the remaining segments are title-cased and joined with
// spaces (NEXT_PUBLIC_APP_SLUG -> App Slug).
func envKeyLabel(key string) string {
	key = strings.TrimPrefix(key, "NEXT_PUBLIC_")
	parts := strings.Split(key, "_")
	for i, part := range parts {
		switch len(part) {
		case 0:
		case 1:
			parts[i] = strings.ToUpper(part)
		default:
			parts[i] = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
		}
	}
	return strings.Join(parts, " ")
}

// vapidPrivateKey derives the private-key variable name paired with a public
// VAPID variable (NEXT_PUBLIC_VAPID_PUBLIC_KEY -> VAPID_PRIVATE_KEY).
func vapidPrivateKey(publicKey string) string {
	name := strings.TrimPrefix(publicKey, "NEXT_PUBLIC_")
	return strings.Replace(name, "PUBLIC_", "PRIVATE_", 1)
}

// slugify turns a project name into a lower-case, dash-separated slug.
func slugify(name string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// isGeneratedEnvKey reports whether key is filled by a generator (application
// encryption key or VAPID pair) and therefore never prompted for.
func isGeneratedEnvKey(key string) bool {
	upper := strings.ToUpper(key)
	switch {
	case upper == "APP_KEY", strings.HasSuffix(upper, "ENCRYPTION_KEY"):
		return true
	case strings.HasSuffix(upper, "VAPID_PUBLIC_KEY"), strings.HasSuffix(upper, "VAPID_PRIVATE_KEY"):
		return true
	default:
		return false
	}
}

// promptEnvValues walks every configurable variable and asks the developer to
// confirm or edit its value. The synthesised (or sample) value is offered as
// the input placeholder, so pressing Tab fills the field with the suggestion;
// leaving the field empty keeps that suggestion. Pressing Esc keeps the
// current/typed value and auto-completes every remaining variable with its
// suggested value. Generated secrets are skipped. Non-interactive runs keep the
// synthesised values as-is.
func promptEnvValues(env *pkg.EnvFile) error {
	if !tui.IsInteractive() {
		return nil
	}
	for _, key := range env.Keys() {
		if isGeneratedEnvKey(key) {
			continue
		}
		current, _ := env.Get(key)
		value, auto, err := tui.AskTextAuto(i18n.Tf("init.prompt.env", envKeyLabel(key)), current)
		if err != nil {
			return err
		}
		if value = strings.TrimSpace(value); value != "" {
			env.Set(key, value)
		}
		if auto {
			// Esc pressed: keep this field and accept the suggested values for
			// all the remaining variables.
			return nil
		}
	}
	return nil
}
