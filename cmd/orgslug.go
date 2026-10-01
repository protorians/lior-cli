package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/protorians/lior-cli/internal/auth"
	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/pkg"
	"github.com/protorians/lior-cli/internal/store"
	"github.com/protorians/lior-cli/internal/tui"
)

// The organization slug operations go through indirections so the tests can
// exercise the connect and create flows offline, where no real session would
// exist and no store would answer.
var (
	orgSlugLookup   = store.ResolveOrganizationSlug
	orgSlugRegister = store.RegisterOrganizationSlug
)

// organizationSlugForCreate resolves the organization slug a module identity is
// built on (`<prefix(type)>.<slug>.<module>`). An explicit --publisher wins —
// the escape hatch of CI runs and scripted replays — otherwise the slug comes
// from the `liora connect` session. Interactive runs enforce the session: the
// domain is composed from the organization the developer is connected to, and
// an account exposing no slug must define one before anything is created.
func organizationSlugForCreate(interactive bool, publisherFlag string) (string, error) {
	if publisher := strings.TrimSpace(publisherFlag); publisher != "" {
		return publisher, nil
	}
	slug, err := orgSlugLookup(context.Background())
	if err == nil && slug != "" {
		return slug, nil
	}
	switch {
	case errors.Is(err, store.ErrNotConnected):
		return "", pkg.NewErrorWithFix(i18n.T("cat.module"),
			i18n.T("create.error.not_connected"),
			i18n.T("create.error.not_connected.fix"), pkg.ExitAuth)
	case err != nil:
		return "", pkg.NewErrorWithFix(i18n.T("cat.module"),
			i18n.Tf("create.error.org_slug_unreachable", err.Error()),
			i18n.T("create.error.org_slug_unreachable.fix"), pkg.ExitNetwork)
	default: // the store answered: the account carries no slug
		if !interactive {
			return "", pkg.NewErrorWithFix(i18n.T("cat.module"),
				i18n.T("create.error.org_slug_missing"),
				i18n.T("create.error.org_slug_missing.fix"), pkg.ExitError)
		}
		return defineOrganizationSlug()
	}
}

// promptOrganizationSlug is the seam the tests replace: the slug definition
// prompt requires a terminal, and the registration logic behind it must stay
// exercisable offline.
var promptOrganizationSlug = promptOrganizationSlugInteractive

// promptOrganizationSlugInteractive asks the developer for their organization
// slug, re-asking until the answer normalizes to a valid kebab-case slug.
func promptOrganizationSlugInteractive() (string, error) {
	return askValidated(i18n.T("connect.prompt.org_slug"), "mon-orga", func(v string) error {
		if len(store.CatalogSlug(v)) < 2 {
			return errors.New(i18n.T("connect.error.org_slug_invalid"))
		}
		return nil
	})
}

// defineOrganizationSlug forces the definition of the organization slug through
// the CLI: an organization without a slug cannot own a canonical module
// identity. The answer is normalized (kebab-case) and registered on the store
// (`POST /developer-store/accounts/register`).
func defineOrganizationSlug() (string, error) {
	s := tui.NewStyles()
	fmt.Fprintln(os.Stderr, s.Hint.Render(i18n.T("connect.org_slug.missing")))
	answer, err := promptOrganizationSlug()
	if err != nil {
		return "", err
	}
	slug := store.CatalogSlug(answer)

	account, rerr := orgSlugRegister(context.Background(), organizationDisplayName(), slug)
	if rerr != nil {
		// A conflict means the account already exists server-side — re-read it:
		// the slug may have been defined in the meantime, from another machine.
		var apiErr *pkg.APIError
		if errors.As(rerr, &apiErr) && apiErr.StatusCode == 409 {
			if existing, lerr := orgSlugLookup(context.Background()); lerr == nil && existing != "" {
				warn(i18n.Tf("connect.org_slug.existing", existing))
				return existing, nil
			}
		}
		return "", pkg.NewErrorWithFix(i18n.T("cat.authentication"),
			i18n.Tf("connect.error.org_slug_register", rerr.Error()),
			i18n.T("connect.error.org_slug_register.fix"), pkg.ExitAuth)
	}
	if account != nil && strings.TrimSpace(account.Slug) != "" {
		slug = strings.TrimSpace(account.Slug)
	}
	warn(i18n.Tf("connect.org_slug.registered", slug))
	return slug, nil
}

// organizationDisplayName derives the account name the register endpoint
// requires from the session user: the username, then the email local part,
// then the store default.
func organizationDisplayName() string {
	sess, err := auth.LoadSession(auth.NewStore())
	if err == nil && sess != nil && sess.User != nil {
		user := *sess.User
		for _, candidate := range []string{user.Name, user.Username, user.Email} {
			if name := strings.TrimSpace(candidate); name != "" {
				if at := strings.Index(name, "@"); at > 0 {
					name = name[:at]
				}
				return name
			}
		}
	}
	return "developer"
}

// ensureConnectOrganizationSlug verifies the organization slug right after the
// sign-in: a reachable store whose account exposes no slug forces its
// definition in an interactive session. Every degraded path — store
// unreachable, non-interactive run — warns and defers to `create module`,
// which enforces the slug before composing a domain.
func ensureConnectOrganizationSlug() string {
	slug, err := orgSlugLookup(context.Background())
	if err != nil {
		if !errors.Is(err, store.ErrNotConnected) {
			warn(i18n.Tf("connect.org_slug.unavailable", err.Error()))
		}
		return ""
	}
	if slug != "" {
		return slug
	}
	if !tui.IsInteractive() {
		warn(i18n.T("connect.org_slug.non_interactive"))
		return ""
	}
	defined, err := defineOrganizationSlug()
	if err != nil {
		warn(err.Error())
		return ""
	}
	return defined
}
