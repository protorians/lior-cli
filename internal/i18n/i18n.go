// Package i18n provides the internationalisation mechanism for the Liora
// CLI. Message catalogs are embedded in locales/*.json (en-US ships as the
// default, fr-FR ships as the first translation) and are selected at runtime
// from, in order of precedence:
//
//  1. the `--lang` root flag,
//  2. the `LIORIAN_CLI_LANG` environment variable,
//  3. the `"cli".lang` key of `lorian.config.json`,
//  4. the OS locale (LC_ALL, LC_MESSAGES, LANG).
//
// Every catalog key falls back to the en-US catalog, then to the key itself,
// so a partial language never breaks the CLI.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
)

//go:embed locales/*.json
var catalogFS embed.FS

// DefaultLang is the fallback catalog (embedded) used for unknown or missing
// messages.
const DefaultLang = "en-US"

// canonical maps a BCP-47 primary language subtag to the canonical catalog
// code shipped with the CLI. Extend this list to add another language.
var canonical = map[string]string{
	"en": "en-US",
	"fr": "fr-FR",
}

var (
	mu       sync.RWMutex
	current  = DefaultLang
	catalogs map[string]map[string]string
)

func init() {
	catalogs = map[string]map[string]string{}
	entries, err := catalogFS.ReadDir("locales")
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		code := strings.TrimSuffix(entry.Name(), ".json")
		data, err := catalogFS.ReadFile("locales/" + entry.Name())
		if err != nil {
			continue
		}
		var catalog map[string]string
		if err := json.Unmarshal(data, &catalog); err != nil {
			continue
		}
		catalogs[code] = catalog
	}
	Use(LangEnvOverride()())
}

// LangEnvOverride returns a resolver that honours LIORIAN_CLI_LANG first,
// then the OS locale variables. Kept as a function so tests can read the
// LIORIAN_CLI_LANG priority precisely.
func LangEnvOverride() func() string {
	return func() string {
		for _, key := range []string{"LIORIAN_CLI_LANG", "LC_ALL", "LC_MESSAGES", "LANG"} {
			if v := strings.TrimSpace(os.Getenv(key)); v != "" {
				return Normalize(v)
			}
		}
		return DefaultLang
	}
}

// AvailableLanguages returns the canonical catalog codes shipped with the CLI.
func AvailableLanguages() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(catalogs))
	for code := range catalogs {
		out = append(out, code)
	}
	sort.Strings(out)
	return out
}

// Use switches the active catalog to lang (normalised) and returns the
// effective catalog code. Unknown or empty codes fall back to DefaultLang.
func Use(lang string) string {
	code := Normalize(lang)
	mu.Lock()
	defer mu.Unlock()
	if _, ok := catalogs[code]; ok {
		current = code
	} else {
		current = DefaultLang
	}
	return current
}

// Language returns the code of the currently active catalog.
func Language() string {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

// Normalize maps an arbitrary locale string ("fr_FR", "FR", "fr-FR",
// "fr-FR.UTF-8", …) to a canonical catalog code. Unrecognised inputs fall back
// to DefaultLang.
func Normalize(locale string) string {
	locale = strings.TrimSpace(locale)
	if locale == "" {
		return DefaultLang
	}
	parts := strings.FieldsFunc(locale, func(r rune) bool {
		return r == '-' || r == '_' || r == '.'
	})
	if len(parts) == 0 {
		return DefaultLang
	}
	tag := strings.ToLower(parts[0])

	mu.RLock()
	defer mu.RUnlock()
	if code, ok := canonical[tag]; ok {
		return code
	}
	for code := range catalogs {
		if strings.HasPrefix(strings.ToLower(code), tag+"-") || strings.EqualFold(code, tag) {
			return code
		}
	}
	return DefaultLang
}

// T returns the localized message for key in the active catalog, falling back
// to the default catalog, then to the key itself.
func T(key string) string {
	mu.RLock()
	defer mu.RUnlock()
	if v := lookup(current, key); v != "" {
		return v
	}
	if v := lookup(DefaultLang, key); v != "" {
		return v
	}
	return key
}

// Tf formats the localized message for key using fmt.Sprintf-style arguments
// (e.g. Tf("init.error.exists", dir)).
func Tf(key string, args ...any) string {
	if len(args) == 0 {
		return T(key)
	}
	return fmt.Sprintf(T(key), args...)
}

func lookup(code, key string) string {
	if catalog := catalogs[code]; catalog != nil {
		return catalog[key]
	}
	return ""
}
