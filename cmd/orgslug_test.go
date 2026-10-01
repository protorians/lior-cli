package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/module"
	"github.com/protorians/lior-cli/internal/pkg"
	"github.com/protorians/lior-cli/internal/store"
)

// stubOrgSlugSeams replaces the organization slug seams for one test and
// restores the real implementations afterwards — the lookups must stay
// exercisable offline, where no real session would exist.
func stubOrgSlugSeams(t *testing.T,
	lookup func(context.Context) (string, error),
	register func(context.Context, string, string) (*store.DeveloperAccount, error),
	prompt func() (string, error)) {
	t.Helper()
	if lookup != nil {
		orgSlugLookup = lookup
	}
	if register != nil {
		orgSlugRegister = register
	}
	if prompt != nil {
		promptOrganizationSlug = prompt
	}
	t.Cleanup(func() {
		orgSlugLookup = store.ResolveOrganizationSlug
		orgSlugRegister = store.RegisterOrganizationSlug
		promptOrganizationSlug = promptOrganizationSlugInteractive
	})
}

func TestOrganizationSlugForCreateFlagWins(t *testing.T) {
	stubOrgSlugSeams(t, func(context.Context) (string, error) {
		t.Error("the session must not be consulted when --publisher is given")
		return "", nil
	}, nil, nil)
	createPublisher = "flag-org"
	defer func() { createPublisher = "" }()

	slug, err := organizationSlugForCreate(false, createPublisher)
	if err != nil {
		t.Fatalf("organizationSlugForCreate: %v", err)
	}
	if slug != "flag-org" {
		t.Errorf("slug = %q, want %q", slug, "flag-org")
	}
}

func TestOrganizationSlugForCreateFromSession(t *testing.T) {
	stubOrgSlugSeams(t, func(context.Context) (string, error) {
		return "session-org", nil
	}, nil, nil)

	slug, err := organizationSlugForCreate(false, "")
	if err != nil {
		t.Fatalf("organizationSlugForCreate: %v", err)
	}
	if slug != "session-org" {
		t.Errorf("slug = %q, want %q", slug, "session-org")
	}
}

func TestOrganizationSlugForCreateNotConnected(t *testing.T) {
	stubOrgSlugSeams(t, func(context.Context) (string, error) {
		return "", store.ErrNotConnected
	}, nil, nil)

	_, err := organizationSlugForCreate(false, "")
	if err == nil {
		t.Fatal("a creation without a session must fail")
	}
	if !strings.Contains(err.Error(), i18n.T("create.error.not_connected")) {
		t.Errorf("error must report the missing session, got: %v", err)
	}
}

func TestOrganizationSlugForCreateUnreachableStore(t *testing.T) {
	stubOrgSlugSeams(t, func(context.Context) (string, error) {
		return "", errors.New("connection refused")
	}, nil, nil)

	_, err := organizationSlugForCreate(false, "")
	if err == nil {
		t.Fatal("an unreachable store must fail the creation")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("error must carry the store failure, got: %v", err)
	}
}

func TestOrganizationSlugForCreateMissingSlugNonInteractive(t *testing.T) {
	stubOrgSlugSeams(t, func(context.Context) (string, error) {
		return "", nil
	}, nil, nil)

	_, err := organizationSlugForCreate(false, "")
	if err == nil {
		t.Fatal("a creation for an organization without a slug must fail")
	}
	if !strings.Contains(err.Error(), i18n.T("create.error.org_slug_missing")) {
		t.Errorf("error must report the missing organization slug, got: %v", err)
	}
}

func TestOrganizationSlugForCreateMissingSlugInteractiveRegisters(t *testing.T) {
	var registeredSlug, registeredName string
	stubOrgSlugSeams(t,
		func(context.Context) (string, error) { return "", nil },
		func(_ context.Context, name, slug string) (*store.DeveloperAccount, error) {
			registeredName, registeredSlug = name, slug
			return &store.DeveloperAccount{ID: "acc-1", Slug: "mon-orga", Name: name}, nil
		},
		func() (string, error) { return "Mon Orga!", nil })

	slug, err := organizationSlugForCreate(true, "")
	if err != nil {
		t.Fatalf("organizationSlugForCreate: %v", err)
	}
	if slug != "mon-orga" {
		t.Errorf("slug = %q, want the normalized %q", slug, "mon-orga")
	}
	if registeredSlug != "mon-orga" {
		t.Errorf("registered slug = %q, want %q", registeredSlug, "mon-orga")
	}
	if registeredName == "" {
		t.Error("the register call must carry a display name")
	}
}

func TestOrganizationSlugForCreateRegisterConflictFallsBackToStore(t *testing.T) {
	lookups := 0
	stubOrgSlugSeams(t,
		func(context.Context) (string, error) {
			// First lookup: the account exposes no slug. Second lookup, after
			// the register conflict: the slug was defined in the meantime.
			lookups++
			if lookups == 1 {
				return "", nil
			}
			return "mon-orga", nil
		},
		func(context.Context, string, string) (*store.DeveloperAccount, error) {
			return nil, fmt.Errorf("failed to register the developer account: %w",
				&pkg.APIError{StatusCode: 409, Message: "already exists"})
		},
		func() (string, error) { return "mon-orga", nil })

	slug, err := organizationSlugForCreate(true, "")
	if err != nil {
		t.Fatalf("organizationSlugForCreate: %v", err)
	}
	if slug != "mon-orga" {
		t.Errorf("slug = %q, want the store-defined %q", slug, "mon-orga")
	}
}

// TestCollectCreateSpecComposesDomainFromSessionSlug verifies the non-interactive
// composition: the canonical domain is built from the effective type, the
// organization slug of the session and the identifier, and the whole identity
// is kept on the spec for the rest of the process.
func TestCollectCreateSpecComposesDomainFromSessionSlug(t *testing.T) {
	stubOrgSlugSeams(t, func(context.Context) (string, error) {
		return "acme", nil
	}, nil, nil)

	spec := module.ModuleSpec{ID: "crm", Type: "SERVICE"}
	if err := collectCreateSpec(&spec); err != nil {
		t.Fatalf("collectCreateSpec: %v", err)
	}
	if spec.Domain != "service.acme.crm" {
		t.Errorf("Domain = %q, want %q", spec.Domain, "service.acme.crm")
	}
	if spec.ID != "crm" {
		t.Errorf("ID = %q, want %q", spec.ID, "crm")
	}
	if spec.Type != "SERVICE" {
		t.Errorf("Type = %q, want %q", spec.Type, "SERVICE")
	}
}

// TestCollectCreateSpecWithoutSessionFails verifies that a non-interactive
// creation which must compose a domain fails explicitly when no session
// provides the organization slug.
func TestCollectCreateSpecWithoutSessionFails(t *testing.T) {
	stubOrgSlugSeams(t, func(context.Context) (string, error) {
		return "", store.ErrNotConnected
	}, nil, nil)

	spec := module.ModuleSpec{ID: "crm"}
	if err := collectCreateSpec(&spec); err == nil {
		t.Fatal("a creation that must compose a domain without a session must fail")
	}
}
