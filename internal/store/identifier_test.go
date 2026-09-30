package store

import (
	"testing"

	"github.com/protorians/lior-cli/internal/module"
)

// offline forces the resolver to skip the authenticated-account source, the
// only way to exercise the manifest-based fallbacks deterministically.
func offline(t *testing.T) {
	t.Helper()
	previous := lookupAuthenticatedSlug
	lookupAuthenticatedSlug = func() string { return "" }
	t.Cleanup(func() { lookupAuthenticatedSlug = previous })
}

func manifestWith(domain, publisherID string) *module.Manifest {
	m := &module.Manifest{Domain: domain, ID: "calendar", Key: "CALENDAR", Version: "1.0.0"}
	m.Publisher.ID = publisherID
	return m
}

func TestResolveModuleIdentifierPublisherSources(t *testing.T) {
	offline(t)
	for _, tc := range []struct {
		name    string
		domain  string
		pubID   string
		want    string
		comment string
	}{
		{
			name: "domain canonique a trois labels", domain: "mod.liorian.calendar",
			want: "mod.liorian.calendar",
			// `CatalogSlug` on the raw domain would collapse the dots into
			// `mod-liorian-calendar`: the split must happen on the raw domain.
			comment: "libellé éditeur repris du domaine",
		},
		{
			name: "domaine a deux labels", domain: "vendor.calendar",
			want: "mod.developer.calendar",
			// Nothing establishes the publisher: the store default applies —
			// and the store will recompute *its own* default, so this is the
			// only value that can still match.
			comment: "défaut du store",
		},
		{
			name: "domaine a quatre labels", domain: "mod.liorian.eu.calendar",
			want:    "mod.developer.calendar",
			comment: "forme non canonique : jamais reinterpretée",
		},
		{
			name: "publisher.id explicite", domain: "mod.liorian.calendar", pubID: "acme",
			want: "mod.acme.calendar",
			// The declared publisher outranks the domain label: the developer
			// says who they are, and the value travels with the module.
			comment: "publisher déclaré prioritaire",
		},
		{
			name: "publisher.id accentué", domain: "vendor.calendar", pubID: "Éditions Démo",
			want:    "mod.editions-demo.calendar",
			comment: "slugifié comme côté store (le préfixe « mod » est littéral)",
		},
		{
			name: "publisher.id en UUID", domain: "mod.liorian.calendar",
			pubID: "19c5e69b-4643-4902-9a9c-f75e8be72455",
			want:  "mod.liorian.calendar",
			// A record identifier is not a catalogue slug: slugifying it would
			// sign an identifier the store can never recompute (422).
			comment: "UUID ignoré au profit du domaine",
		},
		{
			name: "aucune source", domain: "",
			want:    "mod.developer.calendar",
			comment: "défaut du store",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveModuleIdentifier(manifestWith(tc.domain, tc.pubID)); got != tc.want {
				t.Errorf("ResolveModuleIdentifier(%q, publisher=%q) = %q, want %q — %s",
					tc.domain, tc.pubID, got, tc.want, tc.comment)
			}
		})
	}
}

func TestResolveModuleIdentifierPrefersAuthenticatedAccount(t *testing.T) {
	previous := lookupAuthenticatedSlug
	lookupAuthenticatedSlug = func() string { return "acme-dev" }
	t.Cleanup(func() { lookupAuthenticatedSlug = previous })

	// The account is the only value the store will recompute, so it wins over
	// every manifest-declared source — including `publisher.id`.
	if got := ResolveModuleIdentifier(manifestWith("mod.liorian.calendar", "acme")); got != "mod.acme-dev.calendar" {
		t.Errorf("ResolveModuleIdentifier with a session = %q, want %q", got, "mod.acme-dev.calendar")
	}
}

func TestResolveModuleIdentifierIsStable(t *testing.T) {
	offline(t)
	m := manifestWith("mod.liorian.calendar", "")
	first := ResolveModuleIdentifier(m)
	// The identifier is covered by the signature: any drift between two calls
	// would invalidate every signature produced for the same module.
	for range 5 {
		if got := ResolveModuleIdentifier(m); got != first {
			t.Fatalf("ResolveModuleIdentifier is not stable: %q != %q", got, first)
		}
	}
	if first != "mod.liorian.calendar" {
		t.Errorf("ResolveModuleIdentifier = %q, want %q", first, "mod.liorian.calendar")
	}
}
