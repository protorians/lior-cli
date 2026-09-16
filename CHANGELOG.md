# Changelog

All notable changes to this project will be documented in this file.


## [Unreleased]

### Technical Details
- **CI / Release** — le pipeline de release crée désormais une **pre-release GitHub** à chaque push sur les branches `alpha`, `beta` ou `rc` : la version `v<base>-<canal>.<n>` est calculée depuis le `app.config.json` (le suffixe `.<n>` est incrémenté selon les tags `v<base>-<canal>.*` déjà publiés), le tag est poussé automatiquement, la release est marquée pre-release (`prerelease: auto`) et la publication npm reste réservée aux versions stables.


## [v0.3.1] - 2026-09-16

### Fixed
- **Lint (golangci-lint)** — toutes les violations `errcheck` et `unused` sont corrigées : retours de `filepath.Walk`/`filepath.WalkDir` désormais contrôlés, erreurs des écritures de tests vérifiées (helper `mustWrite`, écritures httptest), helper TUI orphelin `ruleLine` supprimé.
- **Audit** — le flag `--output` n'accepte plus que `table` ou `json` ; toute autre valeur est rejetée par une erreur catégorisée (`audit.error.format`) au lieu d'afficher silencieusement le tableau.

### Technical Details
- **CI / Release** — bump des actions GitHub (`actions/upload-artifact` v5→v6, `goreleaser/goreleaser-action` v6→v7) et refonte du pipeline de release en un seul run : la version est résolue depuis l'input du `workflow_dispatch`, le tag `vX.Y.Z` poussé ou le `app.config.json` ; le tag est créé et poussé automatiquement (dispatch ou push sur `main`) avant la construction des binaires GoReleaser, et la publication npm utilise la version résolue au lieu du ref git.


## [v0.3.0] - 2026-09-16

### Added
- **`create module` — vérification des `requirements`** : chaque module requis par le `manifest.json` doit exister localement — dans `external_modules/` **ou** `src/modules/` ; les modules cœur de la plate-forme (`organization`, `identity`) sont toujours satisfaits. En cas de module manquant, la création échoue et le module scaffoldé ainsi que la page éventuelle sont supprimés (rollback), avec une erreur catégorisée listant les modules absents (`create.error.requirements_missing`).
- **`create module` — installation des dépendances** : les `dependencies` et `devDependencies` du manifest sont résolues via le premier gestionnaire de paquets détecté (`bun → pnpm → yarn → npm`) à la racine du projet. Désactivable via `SENTIENT_CLI_SKIP_INSTALL=1` ; un échec d'installation ou l'absence de gestionnaire reste non-bloquant (simple avertissement).
- **Manifest** — prise en charge du champ `devDependencies`, ajouté au mockup embarqué hello-world.

### Changed
- **Audit** — la vérification des requirements réutilise le moteur de résolution commun à `create` (`external_modules/` **ou** `src/modules/`).

### Technical Details
- **CI / Release** — les workflows GitHub sont reconstruits de zéro : pipeline de release déclenché par un tag `vX.Y.Z` (créable aussi via `workflow_dispatch`), build des binaires GoReleaser Windows/macOS/Linux dans `./releases` (archives + binaires nus + `checksums.txt`), publication automatique de la release GitHub dont le sommaire contient les liens de téléchargement des binaires et les détails du changelog (`scripts/release-notes.sh`).

### Docs
- `docs/specs/sentient.md` §5.2 mis à jour (vérification des requirements et installation des dépendances dans le pipeline `create module`) ; `docs/rapport-implementation.md` complété.


## [v0.2.0] - 2026-09-15

### Added
- **`create module --mockup` / `--page-mockup`** — le module et la page sont scaffolés depuis des répertoires/gabarits explicites (priorité sur `SENTIENT_MODULE_MOCKUP` / `SENTIENT_PAGE_MOCKUP`), avec repli silencieux sur les mockups embarqués si la source est inutilisable.

### Changed
- **Mockup hello-world aligné 1:1 sur le socle** — le mockup embarqué est désormais identique au module de référence `sentients-socle/external_modules/hello-world` (API SDK `View.*` / `Activity.*`, `AutoBreadcrumb`) ; les imports obsolètes (`Wrapper`, `WaitingActivity`, `AnimatedContent`) sont retirés.

### Docs
- `docs/specs/sentient.md` réalignée sur le code (arborescence `domain/hello-world.interface.ts`, flags de surcharge des mockups, metadata rel. 0.2.0) ; `docs/rapport-implementation.md` et `README.md` mis à jour.


## [v0.1.0] - 2026-09-15

### Added
- **Internationalisation (i18n)** — interface bilingue `en-US` (défaut) / `fr-FR` : aide, descriptions, prompts, flags et erreurs sont traduits. La langue est résolue dans l'ordre `--lang` → `SENTIENT_CLI_LANG` → `cli.lang` de `sentients.config.json` → locale de l'OS (`LC_ALL`/`LC_MESSAGES`/`LANG`), avec repli sur `en-US`.
- **Registre d'applications embarqué** — `app.config.json` est compilé dans le binaire (`//go:embed`) : chaque commande résout l'URL de base et le timeout des API (`sentient-auth`, `sentient-store`) depuis le registre, surchargeable par un `app.config.json` local remonté des répertoires ou par `SENTIENT_AUTH_API`.
- **`create module` depuis le mockup hello-world** — le module est généré depuis un module de référence embarqué (Clean Architecture : `application/`, `domain/`, `infrastructure/`, `presentation/`, `manifest.json`, `index.tsx`), renommé avec le nom du module ; une page `src/app/<name>/page.tsx` est aussi scaffolée quand le manifest déclare une `uri`/`url`. Surcharges `SENTIENT_MODULE_MOCKUP` et `SENTIENT_PAGE_MOCKUP`.
- **`init` par canal de release** — téléchargement du ZIP de release du template `protorians/sentients-socle` (au lieu d'un clone git) avec `--channel alpha|beta|rc|stable` (stable par défaut) ; les dossiers existants non vides ne sont plus écrasés sans accord explicite.
- **`audit --output table|json`** — sortie machine (JSON) ou tableau pour les audits de modules.
- **`unlink --sync-remote`** — synchronisation des métadonnées locales (nom, type, description) vers le produit distant avant le déliage.
- **Scripts de développement** — `scripts/dev-uninstall.sh` ; `dev-install.sh` gagne `--build-only`, `--install-only` et `--prefix`.
- **TUI** — aide thématisée (wordmark, sections teintées) et composants/styles cohérents, avec repli `--no-color` stable pour les tests E2E.

### Changed
- **Authentification sentient-auth** — la base URL n'est plus codée en dur : résolution `SENTIENT_AUTH_API` → registre workspace → registre embarqué ; le champ `device` devient une chaîne ; timeouts configurés par application (`api.timeout`).
- **Configuration JSON** — `sentients.config.json` remplace le TOML (`.sentient-cli.toml`) avec des clés camelCase ; le parser TOML est retiré.
- **User-Agent standardisé** `Protorians/5.0 (…) Senteints/<version>` sur toutes les requêtes HTTP de la CLI et de l'installeur npm.
- **`pack`** inclut désormais `src/app/<module.url>/` dans l'archive `.smp`.

### Removed
- `scripts/release.sh` — le release passe par les tags git et les workflows CI/GoReleaser.

### Docs
- `docs/specs/sentient.md` réalignée sur le code (i18n FR-025/NFR-007, registre `app.config.json` TECH-009, FR-002/004/008/010/012/014/015) ; `docs/rapport-implementation.md` et `README.md` mis à jour (nouvelle config JSON, commandes et variables d'environnement).


## [v0.0.9] - 2026-09-12

### Changed
- **Alignement API sur les contrats documentés** (`sentient-workspace`) :
  - Enveloppe Raiton `{ message, data, statusCode }` décodée dans `internal/pkg/http.go` (rétro-compat `{code,message}`) ;
  - Auth **à jeton unique** : `POST /api/auth/sign-in` → `{user, token, device}`, `/api/auth/logout`,
    `/api/auth/sessions/refresh` ; le `refresh_token` est retiré du modèle de session ;
  - MFA **gardée** : `POST /api/mfa/challenge` puis `/api/mfa/totp/verify` ou `/api/mfa/recovery/verify`,
    exécutés avec la session après sign-in ;
  - Store sur l'API **developer-store** : `ListModules`/`GetModule`/`UpdateModule`/`Publish` passent par
    `/api/developer-store/modules/**` — la publication suit le pipeline produit → version → artefact
    (checksum SHA-256 hex + signature Ed25519 base64 du `.smp.sig` + poids).

### Docs
- `docs/specs/sentient.md` §5.3/5.4/5.6/5.7, §6.3 et §8 (endpoints + DTOs) réalignés sur les contrats documentés ;
- `docs/rapport-implementation.md` (# connect/disconnect/publish/link) mis à jour.


## [v0.0.8] - 2026-09-12

### Changed
- **Commande renommée `sentient` → `sentients`** : le binaire et toutes les invocations de la CLI utilisent désormais `sentients` (Cobra, GoReleaser, binaire npm, CI).

### Docs
- Mise à jour de la documentation et de la spécification (`README.md`, `docs/specs/sentient.md`, `docs/rapport-implementation.md`) avec la commande `sentients`.


## [v0.0.7] - 2026-09-11

### Features


### Bug Fixes


### Other Changes
- chore(release): add `--no-push` option to `release.sh` for manual push control (70828b3)
- chore(release): remove `[skip ci]` from commit message in `release.sh` (56ac90f)


## [v0.0.6] - 2026-09-11

### Features


### Bug Fixes


### Other Changes
- refactor(ci): replace `version-bump.yml` with `release.sh` (f6171db)

## [v0.0.5] - 2026-09-11

### Features
- feat: update `.goreleaser.yaml` to include binary releases alongside archives (7e9c786)

### Bug Fixes


### Other Changes

## [v0.0.4] - 2026-09-11

### Features
- feat: add comprehensive unit tests and enhance code consistency (39123df)

### Bug Fixes
- fix: add error handling for `os.MkdirAll` in `cmd_test.go` (95f0294)
- fix: add error handling for filesystem operations in tests (b223a43)
- fix: add missing error handling in file and directory operations (4043edd)
- fix: add error handling to tests across multiple packages (9e3fe5d)

### Other Changes
- refactor: improve changelog entry generation in workflows (ef3dbf3)

## [v0.0.3] - 2026-09-10

### Features


### Bug Fixes
- fix: CI Workflow (2c21b14)

### Other Changes


## [v0.0.2] - 2026-09-10

### Features


### Bug Fixes


### Other Changes
- chore: cleanup release workflow and adjust version logic (8996cde)

## [v0.0.1] - 2026-09-10

### Features
- No features


### Bug Fixes
- No bug fixes


### Other Changes
- Maintenance updates


The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

## [0.1.0] - 2026-09-10

### Added

- **init** — Cloner `protorians/sentients-socle` + installer les dépendances (détection bun/pnpm/yarn/npm)
- **create module** — Créer un module standardisé dans `external_modules/`
- **connect** — Authentification via sentient-connect (email + mot de passe, MFA TOTP / backup codes)
- **disconnect** — Invalider le token côté serveur et supprimer les credentials
- **pack** — Construire l'archive `.smp` dans `.sentients/build/`
- **sign keygen** — Générer une paire de clés Ed25519 pour la signature
- **sign [module]** — Signer l'archive `.smp` d'un module (Ed25519)
- **sign verify [module]** — Vérifier la signature d'un module
- **publish** — Auditer, packer et publier un module sur le store
- **link / unlink** — Associer un module local à un module distant du store (token)
- **audit** — Auditer la conformité (Clean Architecture, manifest, dépendances, assets)
- **debug** — Valider le module et lancer un build de diagnostic
- Internal packages: auth, config, module, pkg, tui, signing, audit, debug, store
- NPM wrapper package (`@sentients/cli`)
- GitHub Actions workflows (CI, release, version bump)
- AES-256-GCM encrypted fallback for signing keys (`~/.sentient-cli/signing.enc`)
- OS keychain integration for signing keys via `go-keyring`
