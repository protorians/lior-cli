# Plan de mise à jour / mise à niveau — Sentient CLI

> Objectif : aligner le **code** de la CLI `sentients` sur les contrats de référence du workspace
> (`sentient-workspace/docs`) qui ont évolué, et sur la spec réalignée `docs/specs/sentient.md`.
> Document de travail : à transformer en lots (issues/PR) et à tenir à jour dans
> `docs/rapport-implementation.md`.
>
> Base : branche `alpha`, version courante `0.7.0`, Go `1.26`.
> Références : `docs/modules/module-manifest.md`, `docs/specs/applications/module-distribution.md`,
> `docs/modules/store.md`, `docs/specs/applications/sentient-connect.md`,
> `docs/specs/applications/sentient-oauth.md`, `docs/frontend/view-activity.md`.

---

## État d'avancement (au 2026-09-16)

Légende : ✅ fait · 🟡 partiel · ⬜ non fait · ➖ sans objet.

| Lot | Contenu | Statut |
|-----|---------|--------|
| A | Manifeste conforme + mockup + round-trip (E-01 → E-08) | ✅ fait |
| B | `create module` : `--type`/`--category`, validation (E-12) | ✅ fait (validation `domain` volontairement souple) |
| C | Publish : `token`/`category`/`buildNumber` + mock (E-09, E-10) | ✅ fait |
| D | Audit/Validator étendu (E-11) | ✅ fait |
| E | OAuth refresh + chemin `$schema` (E-13) | ✅ fait (OAuth) · ✅ schéma ; catalogue ➖ |
| F | Docs, i18n, release | ✅ fait |

**Vérifications** : `gofmt -l .` propre · `go vet ./...` propre · `go test ./...` vert ·
`go test ./e2e/ -run TestScripts -count=1` vert · audit sans WARNING sur un module créé neuf.

---

## 0. Constat — écarts spec ↔ code (à résorber)

| # | Écart | Impact | Priorité | Statut |
|---|-------|--------|----------|--------|
| E-01 | `module.Manifest` n'expose pas `optionalRequirements`, `logo`, `banner`, `category`, `configSettings`, `$schema` | Manifeste non conforme au schéma canonique (24 champs requis) | **P0** | ✅ `manifest.go` modelise tous les champs |
| E-02 | `Platform` ne conserve que `supported`/`modes` → `os`, `iosSupported`, `minOsVersion` **perdus** au round-trip (`LoadManifest`→`Save`) et à la publication | Données de plateforme silencieusement effacées | **P0** | ✅ `Platform` étendu + test round-trip |
| E-03 | `Capabilities` ne conserve que 4 drapeaux → `requiresAdmin`, `supportsRealtime`, `processesLocalData` perdus | Idem | **P0** | ✅ `Capabilities` étendu |
| E-04 | `Publisher` (id/name) et `MenuItem` (label/icon/url) incomplets (`url`, `email`, `description`, `avatar` ; `description`, `target`, `keywords`, `items`, `separator`) | Métadonnées perdues | P1 | ✅ champs ajoutés |
| E-05 | `Compatibility.Max` sans `omitempty` → `"max": ""` si absent | Manifeste invalide/non canonique | P1 | ✅ `omitempty` (+ `strict`) |
| E-06 | Mockup `internal/module/mockups/hello-world/manifest.json` sans `$schema` ni `optionalRequirements` | Non conforme / non miroir du socle | **P0** | ✅ miroir 1:1 du socle (`diff` identique) |
| E-07 | Mockup `index.tsx` conserve `requirements`/`dependencies`/`devDependencies` | Viole « manifeste = source de vérité » | P1 | ✅ retirés |
| E-08 | `NewManifest` défauts obsolètes (`managerCompat 0.0.0/*.x`, pas d'`optionalRequirements`/`category`) | Manifeste programme non conforme | P1 | ✅ défauts conformes |
| E-09 | `createProductRequest` n'envoie pas `token`, `description`, `icon`, `secondaryCategory` ; `primaryCategory` = `SYSTEM` codé en dur | Publication non idempotente / fiche produit appauvrie | P1 | ✅ `productMetadata` |
| E-10 | `createVersion` force `buildNumber: 1` | `buildNumber` non incrémenté | P1 | ✅ dernière build + 1 |
| E-11 | Validator/audit ne contrôlent pas `optionalRequirements`, `platforms`, compat, capacités, `category` | Audit partiel vs contrat | P2 | ✅ règles WARNING ajoutées |
| E-12 | `manifest.type` créé = `INTERNAL` (mockup) pour un module tiers | Sémantique de distribution incorrecte | P2 (décision) | ✅ `--type` défaut `EXTERNAL` |
| E-13 | Chemins de schéma incohérents dans le workspace (`module-manifest.schema.json` vs `schemas/module.schema.json`) | `$schema` du manifeste peut ne pas résoudre | P2 (workspace) | ✅ chemin SDK réel figé (`$schema`) |

> **Préservation d'extensions** : ajout d'un sac `Extra map[string]json.RawMessage`
> (`json:"-"`) alimenté par un `UnmarshalJSON` custom et réémis au `MarshalJSON` — aucun
> champ canonique inconnu (`additionalProperties: true`) n'est perdu au round-trip.

---

## 1. Lot A — P0 : conformité du manifeste

**But** : un `manifest.json` produit par la CLI est conforme au schéma canonique et subit un
round-trip **sans perte**.

> **Statut lot A : ✅ Fait.**

### A.1 Étendre `internal/module/manifest.go` — ✅ Fait

- `Manifest` : ajouter `Schema string json:"$schema,omitempty"`, `OptionalRequirements map[string]string json:"optionalRequirements"`, `Logo *string json:"logo,omitempty"`, `Banner *string json:"banner,omitempty"`, `Category string json:"category,omitempty"`, `ConfigSettings []ConfigSetting json:"configSettings,omitempty"`.
- `Platform` : ajouter `OS []string json:"os,omitempty"`, `IOSSupported *bool json:"iosSupported,omitempty"`, `MinOSVersion string json:"minOsVersion,omitempty"`.
- `Capabilities` : ajouter `RequiresAdmin`, `SupportsRealtime`, `ProcessesLocalData` (`bool`, `omitempty`).
- `Publisher` : ajouter `URL`, `Email`, `Description`, `Avatar` (`omitempty`).
- `MenuItem` : ajouter `Description`, `Target`, `Keywords []string`, `Items []MenuItem`, `Separator bool`.
- `Compatibility` : `Max` en `omitempty` ; documenter qu'une plage `max` incomplète est refusée par le schéma (ex. `0.17.0` au lieu de `0.17.x`).
- `ConfigSetting` : `{key, label, type, required, placeholder?, options?}` avec `type` ∈ `TEXT|NUMBER|EMAIL|PASSWORD|URL|TEL|SELECT|TOGGLE`.
- **Préservation d'extensions** : conserver les champs canoniques inconnus. Recommandé : champ `Extra map[string]json.RawMessage` (`json:"-"`) alimenté par un `UnmarshalJSON` custom et réémis au `MarshalJSON`, pour ne plus jamais perdre de champ (`additionalProperties: true`).
- Mettre à jour `NewManifest` (défauts conformes) : `OptionalRequirements: {}`, `Category: "SYSTEM"`, `ConfigSettings: []`, `Compatibility` avec `Max` vide/`omitempty`.

### A.2 Corriger le mockup embarqué — ✅ Fait

- `internal/module/mockups/hello-world/manifest.json` : aligner 1:1 sur
  `frontend/sentient-socle/external_modules/hello-world/manifest.json` → ajouter `$schema`
  (chemin SDK réel) et `optionalRequirements: {}`.
- `internal/module/mockups/hello-world/index.tsx` : retirer `requirements`, `dependencies`,
  `devDependencies` (à ne laisser que dans `manifest.json`).

### A.3 Scaffold — ✅ Fait

- `internal/module/scaffold.go` / `patchManifestIdentity` : ne plus réécrire que l'identité ; laisser
  les nouveaux champs intacts. `patchDeclarationIdentity` ne doit pas réintroduire de `dependencies`.

### A.4 Tests — ✅ Fait

- Nouveau test round-trip (`internal/module/manifest_test.go`) : charger le mockup, `Save`, recharger,
  **égalité stricte** (incl. `os`, `iosSupported`, capacités, menu) → garantit E-02/E-03/E-04.
- `scaffold_test.go` : vérifier `optionalRequirements`, `$schema`, absence de `dependencies` dans `index.tsx`.
- E2E `e2e/testdata/scripts/03_create.txtar` : assertions supplémentaires (`optionalRequirements`, `platforms`).

**DoD lot A** : `go test ./internal/module/...` vert + validateur `ajv` OK sur le manifeste produit.

> ✅ `go test ./internal/module/...` vert. 🟡 le validateur `ajv` n'est pas installé dans
> l'environnement : la conformité a été vérifiée manuellement (24 champs requis, patterns,
> `platforms`/`capabilities`/`publisher`, `$schema`) et le mockup est un miroir 1:1 du socle.
> À rejouer en CI via `ajv` quand disponible.

---

## 2. Lot B — P1 : `create module`

> **Statut lot B : ✅ Fait.** `--type` (défaut `EXTERNAL`) et `--category` (défaut `SYSTEM`)
> ajoutés à `cmd/create.go`, propagés au manifeste et à la déclaration via `ModuleSpec`
> (`EffectiveType`/`EffectiveCategory`), validés par `ValidateType`/`ValidateCategory`.
> Le `index.tsx` scaffolé ne contient plus les 4 champs de prérequis/dépendances (test dédié).

- **Décision E-12** : `type` d'un module créé. Recommandation : `EXTERNAL` par défaut (module tiers
  distribué indépendamment), `INTERNAL` réservé aux modules du socle ; flag `--type` optionnel. — ✅
- Ajouter un défaut `category` (enum `ADMINISTRATION|COMMERCIAL|FINANCE|OPERATIONS|COMMUNICATION|DATA|AUTOMATION|SYSTEM`)
  et un flag `--category`. — ✅
- Valider `id` selon `^[a-z0-9]+(-[a-z0-9]+)*$` et `domain` selon `^mod\.<éditeur>\.[a-z0-9-]+` (aligner
  `ValidateDomain` sur le pattern canonique ; conserver la souplesse reverse-DNS si nécessaire). —
  🟡 `id` conforme ; `ValidateDomain` **reste volontairement reverse-DNS souple** (les fixtures/E2E
  utilisent `com.example.*`) ; l'audit conserve le WARNING de format `mod.sentients.<name>`.
- Vérifier que le scaffold `index.tsx` n'embarque pas les 4 champs de prérequis/dépendances. — ✅

**DoD lot B** : un module créé passe la validation schéma SDK + `sentients audit` sans WARNING de domaine.

> ✅ Vérifié : `audit` sur un module `mod.sentients.<id>` neuf → aucune catégorie WARNING.

---

## 3. Lot C — P1 : publication Developer Store

Fichiers : `internal/store/publisher.go`, `cmd/publish.go`, `e2e/mockapi/mockapi.go`, `publish_test.go`.

> **Statut lot C : ✅ Fait.** `productMetadata` (partagé création/mise à jour) envoie
> `name`, `slug`, `type`, `primaryCategory` (= `manifest.category`, repli `SYSTEM`), `token`,
> `description`, `icon` et `secondaryCategory` (lu dans `Extra`). `createVersion` envoie
> `buildNumber = nextBuildNumber` (dernière build + 1 via `GET …/versions`). Mock E2E
> idempotent par `token` + catégorie/buildNumber. Tests unitaires ajoutés.

- `createProductRequest` : envoyer `token` (`manifest.Token`), `description`, `icon`,
  `secondaryCategory` ; remplacer `defaultPrimaryCategory = "SYSTEM"` par `manifest.Category`
  (repli `SYSTEM`). Le commentaire « manifest format has no category field yet » est obsolète.
- `createVersion` (**E-10**) : `buildNumber` = dernière build + 1 (via `GET .../versions`, déjà
  présent dans `latestVersion`) au lieu de `1` en dur.
- `declareArtifact` : sérialiser le manifeste **avec** les champs complétés (dépend de A.1).
- `UpdateModule` : aligner les métadonnées (catégorie incluses).
- Vérifier la résolution produit (`resolveProduct`) avec `token` envoyé → idempotence réelle.
- Mettre à jour le mock E2E (`e2e/mockapi`) pour honorer `token`/`category`/`buildNumber` et les
  scénarios `publish` (TC-011, TC-012).

**DoD lot C** : republish d'un module lié ne crée pas de doublon produit ; `buildNumber` croît ;
E2E publish verts.

---

## 4. Lot D — P2 : audit / validator

> **Statut lot D : ✅ Fait.** Règles WARNING ajoutées dans `internal/module/validator.go`
> (`optionalRequirements`, `platforms`+`modes`, `managerCompatibility`/`apiCompatibility`
> avec plage `max` complète, `capabilities`, `category`, `publisher`). Test unitaire dédié.

- `internal/module/validator.go` : ajouter des contrôles (WARNING par défaut pour ne pas casser les
  projets existants) :
  - `optionalRequirements` présent (objet) ;
  - `platforms` présent et `modes` requis si `supported: true` ;
  - `managerCompatibility`/`apiCompatibility` présents et plages non incomplètes (`0.17.x`, pas `0.17.0`) ;
  - `capabilities` présent ;
  - `category` dans l'enum si présent ;
  - `publisher` présent.
- Conserver l'audit CLI comme **heuristique locale** ; la conformité de schéma complète reste validée
  en CI via le SDK (déjà noté spec §5.10).

---

## 5. Lot E — P2 : distribution / OAuth / divers

> **Statut lot E : ✅ Fait** (OAuth + `$schema`) · **➖ sans objet** (catalogue).

- **Catalog** : la CLI n'appelle pas `/api/catalog/*` aujourd'hui (normal). Si un jour `link`/`publish`
  doit enrichir depuis le catalogue public, ajouter `sentient-store` au registre (déjà présent) et un
  client dédié. — ➖ aucune action requise (aucun appel catalogue prévu).
- **OAuth** : vérifier si le rafraîchissement d'une session OAuth doit passer par `/oauth/token`
  (`grant_type=refresh_token`, rotation) plutôt que `/api/auth/sessions/refresh` (session legacy).
  À trancher avec `sentient-oauth.md`. — ✅ tranché : `Session.Refresh` privilégie `/oauth/token`
  (`grant_type=refresh_token`, rotation) lorsqu'un refresh token OAuth est stocké, avec repli
  legacy `POST /api/auth/sessions/refresh`.
- **Schéma (`$schema`)** : résoudre l'incohérence de chemin avec le SDK (`schemas/module.schema.json`
  vs `domain/schemas/module-manifest.schema.json`) et fixer la valeur écrite dans les manifestes. —
  ✅ chemin SDK réel figé : `../../node_modules/@sentients/sdk/schemas/module.schema.json`
  (mockup = miroir du socle).

---

## 6. Lot F — P3 : documentation & release — ✅ Fait

> `rapport-implementation.md` (écarts E-01→E-13 clos + itération), `specs/sentient.md`
> (create/publish/audit/OAuth/versions), `CHANGELOG.md` `v0.7.0`, `README.md`,
> `app.config.json` + `npm/sentient-cli/package.json` (0.7.0), i18n fr/en
> (`create.flag.type`/`create.flag.category`, `module.error.type`/`module.error.category`).
> CI vérifiée localement : `gofmt -l`, `go vet`, `go test ./...`, `go test ./e2e/ -run TestScripts`.

- `docs/rapport-implementation.md` : clore les écarts E-01 → E-12 une fois traités.
- `docs/specs/sentient.md` : recompiler les sections concernées si un comportement change.
- `CHANGELOG.md` + `README.md` + `npm/sentient-cli/package.json` (version) + `app.config.json`.
- i18n : nouvelles clés `create.flag.category`, `create.flag.type`, messages d'audit — `en-US.json` **et** `fr-FR.json`.
- CI : `go vet ./...`, `gofmt -l`, `go test ./...`, `go test ./e2e/ -run TestScripts`.

---

## 7. Séquencement & livrables

```
Lot A (manifeste)  ──▶ Lot B (create)  ──▶ Lot C (publish) ──▶ Lot D (audit) ──▶ Lot F (docs/release)
         │                                        ▲
         └──────────────▶ Lot E (OAuth/catalog) ──┘
```

| Lot | Contenu | Estimation | Dépendances | Statut |
|-----|---------|-----------|-------------|--------|
| A | Manifeste conforme + mockup + round-trip | M | — | ✅ |
| B | `create module` (type/category/validation) | S | A | ✅ |
| C | Publish (token/category/buildNumber) | M | A | ✅ |
| D | Audit étendu | S | A | ✅ |
| E | OAuth refresh / schéma | S–M | — | ✅ (+catalogue ➖) |
| F | Docs, i18n, release | S | A–E | ✅ |

> Ordre de priorité recommandé : **A → C** (impacte la conformité et la publication), puis B, D, E, F.

---

## 8. Risques & mitigation

| Risque | Mitigation | État |
|--------|------------|------|
| Le round-trip typé perd des champs futurs | Conserver un sac d'extensions `json.RawMessage` (A.1) | ✅ `Extra` + test |
| E2E cassés par les nouvelles assertions/mock | Mettre à jour `e2e/mockapi` et txtar dans le même lot | ✅ E2E verts |
| Régression sur les projets existants (manifestes sans `optionalRequirements`) | Audit en WARNING ; migration douce via `sentients audit` | ✅ WARNING uniquement |
| Bump `buildNumber` côté serveur déjà géré | Confirmer la règle « défaut = dernière build + 1 » dans `sentient-connect.md` | ✅ confirmé (spec §2.2) |
| Incohérence de chemin de schéma SDK | Trancher avec l'équipe workspace avant de figer `$schema` | ✅ chemin SDK figé |

---

## 9. Checklist globale (Definition of Done)

- [x] `manifest.json` produit conforme à `module-manifest.schema.json` (validation `ajv`).
  - 🟡 `ajv` indisponible localement : conformité vérifiée manuellement ; à rejouer en CI.
- [x] Test round-trip manifeste vert (aucun champ canonique perdu).
- [x] Mockup = miroir du socle (`$schema`, `optionalRequirements`) ; `index.tsx` sans prereqs/deps.
- [x] `create` (type/category/validation) aligné ; audit sans WARNING sur un module neuf.
- [x] `publish` envoie `token`/`category` et incrémente `buildNumber` ; republish idempotent.
- [x] `go test ./...` + `go test ./e2e/ -run TestScripts` + `go vet ./...` verts.
- [x] `CHANGELOG.md`, `README.md`, spec, rapport et i18n à jour.

**Reste à faire (non bloquant)**
- 🟡 Lancer la validation `ajv` en CI sur le manifeste produit (outil absent localement).
- 🟡 Confirmer le durcissement éventuel de `ValidateDomain` vers `^mod\.` en équipe (souplesse
  reverse-DNS conservée pour l'instant).
- ➖ Catalogue `/api/catalog/*` : aucun client à ajouter tant qu'aucun `link`/`publish` ne s'en sert.
