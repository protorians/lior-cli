# Rapport d'implémentation — Sentient CLI

> Document de suivi pour implémenter les features au fil des itérations.
> Dernière mise à jour : 2026-09-12 — version courante du code : `dev` (branche `alpha`).
> Spécification de référence : `docs/specs/sentient.md` (statut *PLANNING*, pourtant largement implémentée).

---

## 1. Vue d'ensemble

La CLI est un binaire Go (module `github.com/protorians/sentient-cli`, **Go 1.26.0**) qui couvre le
cycle de vie :

```
init → create → develop → debug → audit → pack → sign → link → publish
```

Architecture respectée (TECH-006) : `cmd/` (Cobra, présentation) → `internal/*` (services)
→ `internal/pkg` + `internal/config` (infrastructure), rendu TUI via `internal/tui`.

| Élément | État |
|---------|------|
| 13 commandes Cobra (11 de la spec + `sign` à 3 sous-commandes + helper) | ✅ implémentées |
| 10 packages internes (`auth`, `config`, `module`, `signing`, `audit`, `debug`, `store`, `tui`, `pkg`) | ✅ présents |
| Tests unitaires (`go test ./...`) | ✅ verts (9 packages ok) |
| E2E testscript (`go test ./e2e/ -run TestScripts`) | ✅ verts — 10 scénarios, TC-001 → TC-025 (mock `sentient-connect` in-memory) |
| CI/CD GoReleaser + package npm (`@sentients/cli`) | ✅ en place (releases v0.0.1 → v0.0.7) |
| Messages d'erreur français + codes de sortie spec (§11.1) | ✅ respectés |

**Bilan de couverture spec :** les FR-001 → FR-024, NFR-005/006, SEC-001/002/004/005/006/007/008/009
ont une implémentation (parfois partielle). Le reste des FR (001→024) est couvert côté CLI.

---

## 2. Ce qui est implémenté (par commande)

### `sentients init` (FR-001, FR-002, FR-003)
- Clone shallow de `protorians/sentients-socle` (dossier cible demandé, confirmation/écrasement si existe).
- Détection des package managers `bun → pnpm → yarn → npm` (FR-001) + choix interactif.
- Installation des dépendances (non bloquante, simple `warn` en cas d'échec).
- Écrit `sentients.config.json` (config projet).

### `sentients create module [nom]` (FR-004, FR-005)
- Génère la structure `external_modules/<nom>/` : `manifest.json`, `index.tsx`, `README.md`,
  `components/`, `hooks/`, `services/` (avec `.gitkeep`).
- Token UUID v4 dans le manifest (FR-005), key en `UPPER_SNAKE_CASE`.

### `sentients connect` (FR-006, FR-007, FR-008)
- Sign-in email/mot de passe via `POST /api/auth/sign-in` → `{user, token, device}` (**jeton unique**).
- MFA via les endpoints **gardés** `POST /api/mfa/challenge`, `/api/mfa/totp/verify`, `/api/mfa/recovery/verify`
  (le token de session est attaché en Bearer après le sign-in).
- Expiration estimée à 24 h ; rafraîchissement via `POST /api/auth/sessions/refresh` (plus de refresh token).
- Credentials stockées dans le keychain OS (`go-keyring`), avec store chiffré de repli.
- Base URL et timeout via l'entrée `sentient-auth` de `app.config.json` (`api.baseUrl`, `api.timeout`), surchargée par l'env `SENTIENT_AUTH_API` ; le registre `app.config.json` est embarqué dans le binaire.

### `sentients disconnect` (FR-008, FR-009)
- Invalidation serveur best-effort (`POST /api/auth/logout`) + suppression locale, avec confirmation.

### `sentients pack [module]` (FR-010, FR-011)
- Zip `external_modules/<module>/` + `src/app/<module>/` + `public/assets/<module>/` → `.sentients/build/<module>-<version>.smp`.
- Validation préalable du manifest (via `Validator`), limite 50 Mo (`MaxArchiveSize`).

### `sentients sign` (FR-021 → FR-024) — `sign keygen` / `sign <module>` / `sign verify <module>`
- Paires de clés **Ed25519**, stockées dans le keychain (service `sentient-cli-signing`).
- Signature binaire 64 octets dans `<archive>.smp.sig` ; vérification sur archive + clé publique.
- Fingerprint SHA-256 de la clé publique (commande `sign` sans argument).

### `sentients publish [module]` (FR-012, FR-013)
- Authentification obligatoire, auto-audit pré-publication (config `autoAudit`), complétion
  interactive des métadonnées (`name`, `description`, `publisher.*`), pack puis publication en
  **3 étapes** sur l'API developer-store (spec connect §21) :
  1. résolution/création du produit module (`POST /api/developer-store/modules`) ;
  2. création de la version (`POST .../versions`) ;
  3. déclaration de l'artefact (`POST .../versions/:versionId/artifact` : `manifest` +
     checksum SHA-256 + signature `.smp.sig` + `size`).
- Conflit SemVer → bump patch interactif (jusqu'à 5 essais) ; après succès, le manifest local
  est synchronisé (version publiée + **token produit résolu**).

### `sentients link` / `sentients unlink` (FR-014, FR-015)
- `link` : liste les produits modules (`GET /api/developer-store/modules`), valide l'id
  (`GET /api/developer-store/modules/:id`), écrit l'id distant dans le `manifest.json` local et
  **fusionne les métadonnées distantes absentes** (`name`, `description`, `publisher.*`) — §5.7 étape 6.
- Liaison persistée dans un état projet `.sentients/links.json` (nom → id distant) : `LinkedModules`
  ne dépend plus du **format** du token (fini le « UUID ⇒ local » fragile) — pont de migration vers
  l'ancienne heuristique conservé.
- `unlink` : régénère un token UUID local et purge l'état `links.json` (déliaison locale ; pas d'appel
  API de mise à jour).

### `sentients debug [module]` (FR-016)
- Validation + tentative de build de diagnostic (single ou table tous modules), logs formatés.

### `sentients audit [module]` (FR-017, FR-018)
- Audit : Clean Architecture (imports croisés, JSX dans services, index async+render), manifest
  (id/name/version semver/token UUID/entry/domain), index.tsx, requirements, assets.
- Sortie tableau TUI ou JSON (`--output json`), résumé erreurs/warnings.

### `sentients help`, `sentients -v` / `--version` (FR-019, FR-020)
- Aide contextuelle Cobra ; version injectée via ldflags (`main.version/commit/date`).
- Auto-update non bloquant (NFR-006) via GitHub releases (cache 24 h, **notification seule**).

---

## 3. Packages internes

| Package | Rôle | Exports clés |
|---------|------|--------------|
| `auth` | Authentification | `Store`, `Session`, `Connector`, `Authenticator`, `MFAFactor` |
| `config` | Config projet `sentients.config.json` + chemins | `Config`, `Default`, `Load/Save`, `FindProjectRoot`, `ManifestPath` |
| `module` | Logique module | `Manifest`, `Creator`, `Packer`, `Linker`, `Validator` |
| `signing` | Signature Ed25519 | `KeyStore`, `GenerateKeyPair`, `SignArchive`, `VerifySignature`, `Fingerprint`, `FindArchive` |
| `audit` | Audit conformité | `Auditor`, `AuditResult` |
| `debug` | Build de diagnostic | `Debugger`, `DebugResult`, `FormatDebugLogs` |
| `store` | Client store API | `Client` (`ListModules`, `GetModule`, `UpdateModule`, `Publish` — 3 étapes developer-store) |
| `pkg` | Utilitaires | erreurs+exit codes, crypto AES-256-GCM, fs, git, http, uuid, update |
| `tui` | UI Charm | `AskText/AskSecret/Select/Confirm`, `RunWithSpinner`, `Table`, `NewStyles` |

> **Priorité composants TUI (règle obligatoire, spec §9.1)** : avant de créer
> tout composant custom, utiliser en priorité les composants natifs `bubbles/*`
> (spinner, textinput, list, table, viewport, confirm, pager, filepicker, progress,
> help, key, stopwatch, textarea…), puis étendre via lipgloss, puis composer dans
> un modèle Bubbletea. Un composant custom ne doit être implémenté qu'en dernier
> recours, avec justification. Consulter les examples officiels
> https://github.com/charmbracelet/bubbletea/tree/main/examples et la doc
> https://github.com/charmbracelet/bubbles avant chaque nouveau composant TUI.

---

## 4. Écarts, limitations et code incomplet (à corriger en priorité)

> Itération du 2026-09-12 : sécurisation du fallback, code mort supprimé,
> heuristiques fiabilisées, `publish` (conflit SemVer) et `debug` (script du
> module) enrichis. Les §4.1, 4.2-partiel et 4.3-partiel sont désormais traités.
>
> Itération du 2026-09-12 (bis) : `link` fusionne désormais les métadonnées
> distantes absentes (§5.7) ; `LinkedModules` s'appuie sur un état projet
> `.sentients/links.json` (plus de dépendance au format du token) ; le schéma
> `widgets`/`routines`/`menu` du manifest est modélisé ; couleurs TUI
> dérivées de la palette (fin des hex hardcodés hors palette).
>
> Itération du 2026-09-12 (ter) : **alignement API sur les contrats documentés**
> (`sentient-workspace`) — enveloppe Raiton `{message, data, statusCode}` dans
> `pkg/http.go`, préfixe global `/api`, auth à **jeton unique**
> (`POST /api/auth/sign-in`, `/logout`, `/sessions/refresh`), MFA **gardée**
> (`/api/mfa/challenge|totp|recovery` — challenge après sign-in), store sur
> l'API developer-store `POST /api/developer-store/modules/**` (pipeline
> produit → version → artefact avec checksum SHA-256 + signature) ; tests et
> spec §8.1/§8.2 réalignés.
>
> Itération du 2026-09-12 (quater) : **suite E2E testscript** — harnais `e2e/`
> (binaire construit depuis la racine, mock `sentient-connect` in-memory
> **isolé par scénario**, README des scripts), 10 scénarios `01_help_version`
> → `10_link_unlink` couvrant TC-001 → TC-025 avec les codes de sortie
> (§11.1), fixtures `bun/npm/tsc/node`, HOME writable par script (vault
> chiffré), `--no-color` pour des assertions stables ; job CI `e2e` ajouté.
> Prod-ids du mock en UUID (alignés sur le store réel) → le republish passe
> la validation du token et atteint le conflit de version (TC-012, exit 11).

### 4.1 Sécurité — ✅ corrigé à l'itération du 2026-09-12
- **Fallback keychain → fichier chiffré activé** : `auth.NewStore()` et
  `signing.NewKeyStore()` sondent désormais le keychain OS (`keychainAvailable`,
  lecture d'une clé sentinelle) ; s'il est indisponible, elles basculent
  réellement sur le vault chiffré `~/.sentient-cli/credentials.enc` /
  `signing.enc` (spec §7.6, R-002).
- **Passphrases dures supprimées** : les AES utilisaient `"sentient-cli-fallback-v1"` /
  `"sentient-cli-signing-v1"`. Désormais le secret est **aléatoire** (32 octets,
  `pkg.MachineSecret` → `~/.sentient-cli/machine.secret`, 0600) et la clé AES est
  **dérivée par PBKDF2-HMAC-SHA256** (210 000 itérations, sel par message,
  `pkg.EncryptVault`/`DecryptVault`). Aucun secret en dur dans le binaire.
- **Update désactivable en CI** : `SENTIENT_CLI_SKIP_UPDATE` (ou `CI` posée sans
  opt-in `SENTIENT_CLI_UPDATE`) coupe l'appel réseau (`pkg.update.skipUpdate`).
- **Erreurs du challenge MFA remontées** : `mfa.go` ne masque plus un
  `/api/mfa/challenge` en panne — l'erreur est rapportée avec le détail de la
  vérification.
- `NewStoreVolatile` = fallback isolé dans `/tmp` (secret aléatoire, ne pollue
  plus `~/.sentient-cli` en test) ; `NewKeyStoreVolatile` (mort) supprimé.

### 4.2 Régressions de couverture spec (FR)
- **FR-012 `publish`** ✅ : conflit de version géré — `POST` en échec (409 ou
  message de conflit, `isVersionConflict`) ⇒ proposition interactive de bump
  patch (`pkg.BumpPatch`), mise à jour locale du manifest, rebuild + republish
  (jusqu'à 5 essais). Après succès : manifest local synchronisé avec la version
  publiée + sync distante best-effort via `PUT /api/developer-store/modules/:id`
  (`store.Client.UpdateModule`).
- **FR-017/018 `audit`** ✅/partiel :
  - `permissions` doit être un tableau (WARNING) — vérifié sur le JSON brut
    (`rawPermissionsIsArray`, la struct `[]string` ne peut pas matérialiser un
    JSON malformé) ;
  - `dependencies` installées (ERROR) — la **boucle doublon morte** (itération
    sur un `map[string]string`, impossibilité structurelle de doublon) est
    remplacée par la vérification réelle `node_modules/<dep>` ;
  - assets : toujours « dossier non vide » (WARNING).
- **FR-016 `debug`** ✅ : `findBuildCommand` lit d'abord le `package.json` **du
  module** puis la racine, avec un **vrai parsing JSON** des `scripts`
  (fini le `strings.Contains` qui matchait `build` dans `build:prod`). Sans
  script de build, statut **AVERTISSEMENT** (plus de faux « OK » sans build).

### 4.3 Heuristiques fragiles (fiabilité)
- **`Validator.containsDefaultExport`** ✅ : regex élargie
  `export default` + (déclaration | `function Foo` | `async () =>` | `() =>` |
  `class Foo` | `{…}`) ; ne matche plus `export { default } from …`.
- **SemVer** ✅ : regex stricte SemVer 2.0.0 (zéro non significatif rejeté,
  pré-release/meta gérés) ; `pkg.BumpPatch` pour l'incrément patch.
- **Audit Clean Architecture** ✅/partiel : services→JSX affiné avec une regex
  de balises JSX (`jsxTagRE`) qui ignore `Array<string>` tout en attrapant
  `</div>`, `<Foo/>`, `<div className=…/>`. Pas de vrai parseur TS/JSX.
- **`Linker.LinkedModules`** ✅ : les liaisons sont persistées dans
  `.sentients/links.json` (nom → token distant) ; la distinction local/distant
  ne repose plus sur le format de chaîne du token (un token distant au format
  UUID est désormais reconnu). Pont de migration vers l'ancienne heuristique
  conservé pour les projets liés avant l'état.

### 4.4 Schéma & présentation — ✅ traité à l'itération du 2026-09-12 (bis)
- **`widgets`/`routines`/`menu` modélisés** : `manifest.go` remplace les
  `json.RawMessage` opaques par les types `Widget`, `Routine` et
  `MenuItem` (`Menu.Items`) — sérialisation `[]` préservée, round-trip testé.
- **Couleurs TUI unifiées** : plus de hex hardcodés hors palette — le
  sélecteur (`prompts.go`) utilise `palette.accent` pour l'item sélectionné et
  le défaut terminal pour les items normaux (fini le blanc figé, illisible en
  thème clair) ; `TableRow` passe en `muted` (adapté light/dark). Seul
  `ErrorBar` garde un blanc sur fond coloré (contraste, pas une couleur de
  palette).

---

## 5. Roadmap — alignement spec (§13) et prochaines itérations suggérées

La spec découpe 3 releases. État actuel : quasi tout le « MVP » et le « Store » sont implémentés.

### Release 0.1.0 (MVP) — ✅ largement faite
`init` ✅ · `create module` ✅ · `connect` (email/password) ✅ · `connect` (MFA) ✅ · `disconnect` ✅ ·
`pack` ✅ · `-v`/`help` ✅

### Release 0.2.0 (Store) — ✅ largement faite
`publish` ✅ (dont conflit SemVer + PUT) · `link` ✅/partiel · `unlink` ✅ · `audit` ✅/partiel ·
`debug` ✅/partiel · `sign keygen/sign/verify` ✅

### Release 0.3.0 (Qualité) — ⏳ à faire
- S-013 mode verbose/logs ✅ déjà présent (`--verbose`, `SENTIENT_CLI_DEBUG`).
- S-014 config `sentients.config.json` ✅ déjà présente.
- S-015 auto-update ✅ partiel (notification seule, pas de download ; désactivable en CI).
- S-016/017/018 tests unitaires + E2E (testscript) + CI — unitaires ✅ (10 packages), **E2E ✅** (10 scénarios
  txtar, TC-001 → TC-025, mock `sentient-connect` in-memory), **CI ✅** (job `e2e`).

### Prochaines itérations proposées (par priorité)
1. **Sécurité/robustesse** — ✅ fait au 2026-09-12 : fallback keychain↔fichier chiffré
   avec détection réelle, secret machine aléatoire + PBKDF2 (plus de passphrases dures),
   update désactivable en CI, erreurs MFA remontées.
2. **Code mort / incomplet** — ✅ fait : doublons-deps audit remplacés par « deps
   installées », `permissions` array, `SignResult` supprimé, `APIError.Error()`,
   description dans `index.tsx`.
3. **Fiabiliser l'audit & validator** — ✅/partiel : regex default-export élargie,
   semver strict + `BumpPatch`, heuristique JSX services affinée.
4. **`publish` env.** — ✅ fait : conflit de version (bump SemVer) + pipeline developer-store
   (produit → version → artefact) + synchronisation du manifest local (version + token produit).
5. **`debug` env.** — ✅/partiel : script du `package.json` du module (parsing JSON,
   repli racine), plus de faux « OK » sans build réel.
6. **Candidats restants** :
   - testscript E2E (S-017) + CI sur scénarios TC-001 → TC-025 — ✅ fait : `e2e/` (mock
     `sentient-connect` in-memory, 10 scénarios `01_help_version` → `10_link_unlink` couvrant
     TC-001 → TC-025, fixtures `bun/npm/tsc/node`, job CI `e2e`) ;
   - `disconnect`/`unlink` : option de mise à jour distante via `PUT /api/developer-store/modules/:id` (✅
     `unlink --sync-remote` couvert par TC-014) ;
   - bâtir un vrai build de module dans `debug` (au-delà du `package.json`).
7. **Telese spec** : `sentients test <module>`, `sentients watch` (hot-reload), `sentients deploy`,
   `sentients auth` (OAuth2 PKCE), `sentients marketplace` (§2.4 future scope).

---

## 6. Commandes utiles

```bash
go build -o sentients .
./sentients --help
go test ./...              # unitaires + E2E testscript (TC-001 → TC-025)
go test ./e2e/ -run TestScripts -v   # suite E2E seule
go vet ./...
goreleaser release --clean   # release multi-plateforme
```

Couverture de test : unitaires ✅ (10 packages ok) + **E2E ✅** (`e2e/` : `TestMain` construit la CLI
depuis la racine repo, mock `sentient-connect` in-memory dans `e2e/mockapi/`, 10 scripts txtar
`e2e/testdata/scripts/01_help_version.txtar` → `10_link_unlink.txtar` couvrant TC-001 → TC-025, fixtures
exécutables `e2e/testdata/fixtures/bin/{bun,npm,tsc,node}`, job CI `e2e`).