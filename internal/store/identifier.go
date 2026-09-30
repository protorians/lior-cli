package store

import (
	"context"
	"regexp"
	"strings"

	"github.com/protorians/lior-cli/internal/auth"
	"github.com/protorians/lior-cli/internal/module"
)

// lookupAuthenticatedSlug is the seam the tests replace: resolving the
// publisher slug must be exercisable offline, where a developer's real session
// would otherwise decide the outcome of the test.
var lookupAuthenticatedSlug = authenticatedPublisherSlug

// ResolveModuleIdentifier returns the canonical catalogue identifier the store
// recomputes when it verifies an artefact: `mod.<publisherSlug>.<moduleSlug>`
// (spec §7.1 — the identifier is *covered by the signature*).
//
// One resolver for the whole chain. `pack`, `sign`, `sign verify`, `publish`
// and the local installer must all produce the same bytes, or a signature that
// verifies locally is rejected at publication — and again in the socle, which
// re-verifies the relayed attestation against the catalogue identifier (E-007).
// They used to disagree: the packer and the publisher signed the catalogue
// identifier, `sign verify` recomputed `manifest.domain`, and the local
// installer hardcoded the `developer` publisher — so every offline signature
// read as "invalid".
//
// The publisher slug is resolved in decreasing order of authority:
//  1. the authenticated developer account — the only value the store will
//     recompute;
//  2. `publisher.id` from the manifest, which the developer declares
//     themselves and which stays stable across machines;
//  3. the publisher label already carried by `manifest.domain` when it is in
//     the canonical `mod.<publisher>.<module>` form (first-party modules
//     published as `mod.liorian.*`) ;
//  4. the store's own default (`developer`), applied by CatalogIdentifier.
//
// This never fails: a pack performed offline must stay verifiable offline, so
// an unreachable store degrades the slug rather than aborting the build.
func ResolveModuleIdentifier(m *module.Manifest) string {
	return CatalogIdentifier(resolvePublisherSlug(m), ProductSlug(m))
}

// resolvePublisherSlug returns the publisher slug of a module, or "" when none
// can be established (CatalogIdentifier then applies the store default).
func resolvePublisherSlug(m *module.Manifest) string {
	if slug := lookupAuthenticatedSlug(); slug != "" {
		return slug
	}
	if m == nil {
		return ""
	}
	if id := CatalogSlug(m.Publisher.ID); id != "" && !uuidLikeRE.MatchString(id) {
		return id
	}
	// Split the raw domain, never the slugified one: `CatalogSlug` collapses
	// the dots, and `mod.liorian.calendar` would arrive as a single label.
	if labels := strings.Split(strings.ToLower(strings.TrimSpace(m.Domain)), "."); len(labels) == 3 &&
		labels[0] == catalogIdentifierNamespace {
		return CatalogSlug(labels[1])
	}
	return ""
}

// authenticatedPublisherSlug returns the slug of the connected developer
// account, or "" when the CLI is offline or the lookup fails.
func authenticatedPublisherSlug() string {
	sess, err := auth.LoadSession(auth.NewStore())
	if err != nil || sess == nil || !sess.IsAuthenticated() {
		return ""
	}
	client := NewClient()
	client.SetToken(sess.AccessToken)
	client.WithAutoRefresh(sess)
	account, err := client.GetMyAccount(context.Background())
	if err != nil || account == nil {
		return ""
	}
	return account.Slug
}

// uuidLikeRE matches a UUID-shaped value. A UUID in `publisher.id` is an
// internal record identifier, never a catalogue slug: slugifying it would
// produce `mod.19c5e69b-….calendar` and guarantee a `422` at publication, so
// it is discarded in favour of the next source of authority.
var uuidLikeRE = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
