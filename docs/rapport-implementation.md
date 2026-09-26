# Rapport d'implémentation — Liora CLI

> Document de suivi pour implémenter les features au fil des itérations.
> Dernière mise à jour : 2026-09-20 — version courante du code : `v0.14.0` (branche `alpha`).
> Spécification de référence : `docs/specs/liora.md` (statut *active* — implémentée, dernière release documentée 0.14.0).

---

## 1. Vue d'ensemble

La CLI est un binaire Go (module `github.com/protorians/lior-cli`, **Go 1.26.0**) qui couvre le
cycle de vie :

```
init → create → develop → debug → audit → pack → sign → link → publish
```

Architecture respectée (TECH-006) : `cmd/` (Cobra, présentation) → `internal/*` (services)
→ `internal/pkg` + `internal/config` (infrastructure), rendu TUI via `internal/tui`.

| Élément | État |
|---------|------|
| 15 commandes Cobra (13 de la spec + `sign` à 3 sous-commandes + helper) | ✅ implémentées |
| 13 packages internes (`appconfig`, `auth`, `config`, `i18n`, `module`, `signing`, `audit`, `debug`, `moduletest`, `runner`, `store`, `tui`, `pkg`) | ✅ présents |
| Tests unitaires (`go test ./...`) | ✅ verts (15 packages ok) |
| E2E testscript (`go test ./e2e/ -run TestScripts`) | ✅ verts — 13 scénarios, TC-001 → TC-029 (mock `liorian-connect` in-memory) |
| CI/CD GoReleaser + package npm (`/cli`) + formula Homebrew (`Formula/`) | ✅ en place (releases v0.0.1 → v0.13.0, tap `protorians/lior-cli`) |
| Messages d'erreur français + codes de sortie spec (§11.1) | ✅ respectés |

**Bilan de couverture spec :** les FR-001 → FR-024, NFR-005/006, SEC-001/002/003/004/005/006/007/008/009
ont une implémentation (parfois partielle). Le reste des FR (001→024) est couvert côté CLI.

---

## 2. Ce qui est implémenté (par commande)

### `liora init` (FR-001, FR-002, FR-003)
- Clone shallow de `protorians/liorian-socle` (dossier cible demandé, confirmation/écrasement si existe).
- Détection des package managers `bun → pnpm → yarn → npm` (FR-001) + choix interactif.
- Installation des dépendances (non bloquante, simple `warn` en cas d'échec).
- Écrit `lorian.config.json` (config projet).

### `liora create module [nom]` (FR-004, FR-005)
- Génère la structure `library/modules/<nom>/` : `manifest.json`, `index.tsx`, `README.md`,
  `components/`, `hooks/`, `services/` (avec `.gitkeep`).
- Token UUID v4 dans le manifest (FR-005), key en `UPPER_SNAKE_CASE`.
- Vérification des `requirements` (modules requis présents dans `library/modules/` ou `src/modules/` ;
  les modules cœur `organization`/`identity` toujours satisfaits) avec **rollback** en cas de module
  manquant, puis résolution des `dependencies`/`devDependencies` via le gestionnaire de paquets détecté
  (`bun → pnpm → yarn → npm`) à la racine du projet (`--skip-install` pour désactiver).

### `liora connect` (FR-006, FR-007, FR-008)
- Sign-in email/mot de passe via `POST /api/auth/sign-in` → `{user, token, device}` (**jeton unique**).
- MFA via les endpoints **gardés** `POST /api/mfa/challenge`, `/api/mfa/totp/verify`, `/api/mfa/recovery/verify`
  (le token de session est attaché en Bearer après le sign-in).
- Expiration estimée à 24 h ; rafraîchissement via `POST /api/auth/sessions/refresh` (plus de refresh token).
- Credentials stockées dans le keychain OS (`go-keyring`), avec store chiffré de repli.
- Base URL et timeout via l'entrée `liorian-auth` de `app.config.json` (`api.baseUrl`, `api.timeout`), surchargée par l'env `LIORIAN_AUTH_API` ; le registre `app.config.json` est embarqué dans le binaire.

### `liora disconnect` (FR-008, FR-009)
- Invalidation serveur best-effort (`POST /api/auth/logout`) + suppression locale, avec confirmation.

### `liora auth` (spec §5.14 — ex périmètre futur)
- Flux OAuth2 **code d'autorisation + PKCE** (RFC 7636) : `code_verifier`/`code_challenge` S256 +
  `state` anti-CSRF, navigation navigateur, redirection reçue sur un serveur local en boucle
  (`http://127.0.0.1:<port>/callback`, port éphémère), échange du code au `tokenEndpoint`
  (POST form-encoded), stockage de la session (access token + refresh token + expiration) dans
  le keychain (`KeyOAuthRefreshToken` ajouté au store, purgé au `disconnect`).
- Config `oauth` de `liorian-auth` dans `app.config.json` (`authorizationEndpoint`,
  `tokenEndpoint`, `revokeEndpoint`, `clientId`, `scopes`) avec défauts côté `appconfig.OAuth`.
- Mode CI / headless : `LIORIAN_CLI_AUTH_CODE` fournit le code directement (pas de navigateur
  ni de serveur local) ; sans code en non-interactif → erreur catégorisée (exit 2).
- `pkg.OpenBrowser` (cross-platform `open` / `rundll32` / `xdg-open`) ; l'échange de token est
  tolérant (JSON OAuth brut **ou** enveloppe Raiton).

### `liora pack [module]` (FR-010, FR-011)
- Zip `library/modules/<module>/` + `src/app/<module>/` + `public/assets/<module>/` → `.lorian/build/<module>-<version>.SenMod`.
- Validation préalable du manifest (via `Validator`), limite 50 Mo (`MaxArchiveSize`).

### `liora sign` (FR-021 → FR-024) — `sign keygen` / `sign <module>` / `sign verify <module>`
- Paires de clés **Ed25519**, stockées dans le keychain (service `lorian-cli-signing`).
- Signature binaire 64 octets dans `<archive>.SenMod.sig` ; vérification sur archive + clé publique.
- Fingerprint SHA-256 de la clé publique (commande `sign` sans argument).

### `liora publish [module]` (FR-012, FR-013)
- Authentification obligatoire, auto-audit pré-publication (config `autoAudit`), complétion
  interactive des métadonnées (`name`, `description`, `publisher.*`), pack puis publication en
  **3 étapes** sur l'API developer-store (spec connect §21) :
  1. résolution/création du produit module (`POST /api/developer-store/modules`) ;
  2. création de la version (`POST .../versions`) ;
  3. déclaration de l'artefact (`POST .../versions/:versionId/artifact` : `manifest` +
     checksum SHA-256 + signature `.SenMod.sig` + `size`).
- Conflit SemVer → bump patch interactif (jusqu'à 5 essais) ; après succès, le manifest local
  est synchronisé (version publiée + **token produit résolu**).

### `liora link` / `liora unlink` (FR-014, FR-015)
- `link` : liste les produits modules (`GET /api/developer-store/modules`), valide l'id
  (`GET /api/developer-store/modules/:id`), écrit l'id distant dans le `manifest.json` local et
  **fusionne les métadonnées distantes absentes** (`name`, `description`, `publisher.*`) — §5.7 étape 6.
- Liaison persistée dans un état projet `.lorian/links.json` (nom → id distant) : `LinkedModules`
  ne dépend plus du **format** du token (fini le « UUID ⇒ local » fragile) — pont de migration vers
  l'ancienne heuristique conservé.
- `unlink` : régénère un token UUID local et purge l'état `links.json` (déliaison locale ; pas d'appel
  API de mise à jour).

### `liora debug [module]` (FR-016)
- Validation du module + détection du gestionnaire de paquets + résolution de la commande de build
  (script `debug`/`dev`/`build` du `package.json`, repli bundler `esbuild`/`tsup`, repli `tsc --noEmit`).
- Trace **pas-à-pas** des étapes (`internal/tui/step.go` : `RUNNING`/`SUCCESS`/`NOTICE`/`WARNING`/
  `ERROR`/`DEPRECATED`), mise à jour en place de l'étape de build et **streaming** live du
  `stdout`/`stderr` (queue de 8 lignes).
- **Fenêtres d'exécution** : un script `debug`/`dev` (serveur dev/watch) est arrêté après une fenêtre
  de démarrage (15 s) et signalé démarré ; un build one-shot est plafonné (5 min) — `--timeout`
  ajuste les deux.
- **Annulation** `Ctrl+C`/`Esc`/`SIGINT` : arrêt de l'arbre de process (groupe dédié, SIGINT puis
  SIGKILL), carte de confirmation, code de sortie **130**.
- Clôture par un **récapitulatif de sévérité** (`Summary`) ; mode all modules : tableau + logs +
  récapitulatif global.

### `liora audit [module]` (FR-017, FR-018)
- Audit : Clean Architecture (imports croisés, JSX dans services, index async+render), manifest
  (id/name/version semver/token UUID/entry/domain), index.tsx, requirements, assets.
- Sortie tableau TUI ou JSON (`--output json`), résumé erreurs/warnings.

### `liora test [module]` (first chunk of §2.4 future scope, v0.9.0)

- Nouveau package **`internal/moduletest`** (`Tester`, `TestResult`, catalogue `catalog.go`) :
  validation du module + résolution du package de test.
- Gestionnaire de paquets : **choisi à l'installation** (`project.packageManager`), puis surcharge
  `test.packageManager`, puis détection PATH (bun → pnpm → yarn → npm).
- Résolution du package de test par priorité : `--runner`, config
  (`test.runner` / `test.modules.<domaine>.runner`, sentinelles `script`/`builtin`), script `test`
  du `package.json` (module puis projet), catalogue installé (`vitest run`,
  `jest --ci --runInBand`, `mocha`, `ava`), runner intégré (`bun test`) — **uniquement si le module
  contient des fichiers ou dossiers de test** (fichiers `*.test.*` / `*.spec.*`, ou dossier
  `__tests__/`, `test/`, `tests/`, `spec/`, `specs/`) ; sinon `WARNING` (`no_test_script`), jamais
  un faux « OK ».
- **Skip sans fichier ni dossier de test** : un module sans fichier ni dossier de test est
  **ignoré** (statut `SKIPPED`, étape `NOTICE`, `test.no_tests`) même si un script ou un runner est
  configuré — la commande n'est pas lancée, ce qui évite l'échec « No test files found » du runner
  et le faux `ERROR`.
- **Installation** : un package configuré mais absent est installé en dépendance de développement
  dans le périmètre du gestionnaire (`pkg.DevDependencyArgs`). **Sélection interactive** (code
  principal, état d'installation, package personnalisé) **avant** la trace pas-à-pas.
- **Persistance** dans `lorian.config.json` (bloc `test` : `packageManager`, `runner`,
  `modules`) via `TestConfig.RunnerFor`/`SetRunner`.
- **Streaming** live de la sortie de test (queue de 8 lignes), **plafond** 2 min par défaut
  (dépassement → `ERROR`), **annulation** `Ctrl+C`/`Esc`/`SIGINT` (arbre de process, exit **130**).
- **Exit code `13`** (échec de tests, spec §11.1) : le run échoue dès qu'un module a un statut
  `ERROR`. Mode all modules : tableau + logs + récapitulatif global (`Summary`).
- Factorisation : le streaming `stdout`/`stderr` avec groupe de process dédié, fenêtres de démarrage,
  plafonds et arrêt SIGINT→SIGKILL est extrait dans **`internal/runner`** (partagé avec le `debug`,
  désormais allégé de ses helpers `runStream`/`scanLines`/proc).

### `liora help`, `liora -v` / `--version` (FR-019, FR-020)
- Aide contextuelle Cobra ; version/branch/commit/date alignées sur `app.config.json`, surchargées
  par ldflags au build de release (`main.version/branch/commit/date`).
- **Auto-update (S-015, NFR-006)** ✅ — non bloquant, **notification seule** via les releases
  GitHub (cache 24 h) : une version plus récente affiche `Update available: vX → vY` + lien vers
  la page de releases ; **aucun téléchargement automatique** de la nouvelle version n'existe
  (« pas de mise à jour forcée »). Endpoint surchargeable par `LIORIAN_CLI_UPDATE_URL` (tests,
  miroirs) et désactivable par `LIORIAN_CLI_SKIP_UPDATE` / `CI` (opt-in `LIORIAN_CLI_UPDATE`).

---

## 3. Packages internes

| Package | Rôle | Exports clés |
|---------|------|--------------|
| `auth` | Authentification | `Store`, `Session`, `Connector`, `Authenticator`, `MFAFactor` |
| `appconfig` | Registre d'applications embarqué (`app.config.json`, TECH-009) | `Applications`, `BaseURL`, `OAuth` |
| `i18n` | Internationalisation (NFR-007) | `T`, `Tf`, `SetLanguage`, catalogues `en-US`/`fr-FR` |
| `config` | Config projet `lorian.config.json` + chemins | `Config`, `Default`, `Load/Save`, `FindProjectRoot`, `ManifestPath` |
| `module` | Logique module | `Manifest`, `Creator`, `Packer`, `Linker`, `Validator` |
| `signing` | Signature Ed25519 | `KeyStore`, `GenerateKeyPair`, `SignArchive`, `VerifySignature`, `Fingerprint`, `FindArchive` |
| `audit` | Audit conformité | `Auditor`, `AuditResult` |
| `debug` | Build de diagnostic pas-à-pas | `Debugger`, `DebugResult`, `Step`, `FormatDebugLogs` |
| `moduletest` | Exécution des tests pas-à-pas | `Tester`, `TestResult`, `FormatTestLogs` |
| `runner` | Streaming d'exécution partagé (groupe de process, fenêtre/plafond, arrêt) | `Run`, `Outcome`, `OutputLine` |
| `store` | Client store API | `Client` (`ListModules`, `GetModule`, `UpdateModule`, `Publish` — 3 étapes developer-store) |
| `pkg` | Utilitaires | erreurs+exit codes, crypto AES-256-GCM, fs, git, http, uuid, update |
| `tui` | UI Charm | `AskText/AskSecret/Select/Confirm`, `RunWithSpinner`, `RunWithSteps`, `Step`, `Summarize`, `Table`, `NewStyles` |

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
> `.lorian/links.json` (plus de dépendance au format du token) ; le schéma
> `widgets`/`routines`/`menu` du manifest est modélisé ; couleurs TUI
> dérivées de la palette (fin des hex hardcodés hors palette).
>
> Itération du 2026-09-12 (ter) : **alignement API sur les contrats documentés**
> (`liorian-workspace`) — enveloppe Raiton `{message, data, statusCode}` dans
> `pkg/http.go`, préfixe global `/api`, auth à **jeton unique**
> (`POST /api/auth/sign-in`, `/logout`, `/sessions/refresh`), MFA **gardée**
> (`/api/mfa/challenge|totp|recovery` — challenge après sign-in), store sur
> l'API developer-store `POST /api/developer-store/modules/**` (pipeline
> produit → version → artefact avec checksum SHA-256 + signature) ; tests et
> spec §8.1/§8.2 réalignés.
>
> Itération du 2026-09-12 (quater) : **suite E2E testscript** — harnais `e2e/`
> (binaire construit depuis la racine, mock `liorian-connect` in-memory
> **isolé par scénario**, README des scripts), 10 scénarios `01_help_version`
> → `10_link_unlink` couvrant TC-001 → TC-025 avec les codes de sortie
> (§11.1), fixtures `bun/npm/tsc/node`, HOME writable par script (vault
> chiffré), `--no-color` pour des assertions stables ; job CI `e2e` ajouté.
> Prod-ids du mock en UUID (alignés sur le store réel) → le republish passe
> la validation du token et atteint le conflit de version (TC-012, exit 11).
>
> Itération du 2026-09-15 : **mockup hello-world aligné 1:1 sur le socle**
> (le mockup embarqué est identique à `liorian-socle/library/modules/hello-world`
> : composants `View.*` / `Activity.*`, `AutoBreadcrumb`, suppression des
> `Wrapper/Header/Main/Footer` et `WaitingActivity`/`AnimatedContent`) ;
> `create module` expose les flags **`--mockup` / `--page-mockup`** (priorité
> sur les variables d'env, repli sur les mockups embarqués avec warning) ;
> spec §5.2 réalignée (arborescence `domain/hello-world.interface.ts`, flags,
> metadata rel. 0.2.0).
>
> Itération du 2026-09-16 : **sécurisation token refresh (SEC-003) + audit domain
> (spec §5.10)** — `pkg.Client.Do` détecte désormais les réponses HTTP 401 et
> appelle un `TokenRefreshFunc` optionnel pour rafraîchir le bearer token avant
> de retenter la requête une fois (rotation automatique, spec §5.3/SEC-003) ;
> `store.Client.WithAutoRefresh(sess)` connecte le rafraîchissement aux commandes
> `publish`, `link` et `unlink --sync-remote` (les seules qui effectuent des
> appels API authentifiés) ; le check d'audit `domain` (spec §5.10, WARNING)
> vérifie désormais le format attendu `mod.liorian.<name>` au lieu d'accepter
> toute forme reverse-DNS valide ; tests E2E et unitaires mis à jour (domaine
> `mod.liorian.*` dans les fixtures audit).
>
> Itération du 2026-09-16 (bis) : **alignement de la spec sur les docs de référence
> du workspace** (`liorian-workspace/docs`) — manifeste de module conforme au
> schéma canonique `module-manifest.schema.json` (24 champs requis : identité,
> plateformes, compatibilité, permissions/apiScopes, capacités, activation,
> `optionalRequirements`, dépendances), `create module` décrit par domaine
> reverse-DNS + identifiant kebab-case, séparation manifeste/déclaration
> (requirements/dependencies hors `index.tsx`), distribution Connect (developer-store)
> / Store (`/catalog`) / Core (activation) en §8, module OAuth (code + PKCE) en §5.14,
> règle d'audit `domain directory` (WARNING) et scénarios E2E `11_auth` (TC-026/027).
> **Écarts code connus** : le mockup embarqué `hello-world/manifest.json` n'inclut pas
> encore `$schema` ni `optionalRequirements` (contrat canonique), et `index.tsx`
> conserve `requirements`/`dependencies` (à déplacer dans le seul manifeste).
>
> Itération du 2026-09-16 (ter) : **exécution du plan de mise à niveau
> (`docs/plan-mise-a-niveau.md`)** — les écarts E-01 → E-12 sont clos :
> - **A/E-01..E-05** : `module.Manifest` modélise `$schema`, `optionalRequirements`,
>   `logo`, `banner`, `category`, `configSettings`, les capacités étendues
>   (`requiresAdmin`, `supportsRealtime`, `processesLocalData`), les métadonnées
>   éditeur (`url`/`email`/`description`/`avatar`), les items de menu (description,
>   target, keywords, items, separator) et les `platforms` (`os`, `iosSupported`,
>   `minOsVersion`) ; un sac d'extensions `Extra` (`json.RawMessage`) préserve tout
>   champ canonique inconnu au round-trip.
> - **A/E-06..E-08** : mockup embarqué aligné sur le socle (`$schema` SDK réel,
>   `optionalRequirements: {}`, `index.tsx` sans prerequisites/dependencies),
>   défauts `NewManifest` conformes (compat `min` seule, catégorie `SYSTEM`).
> - **B/E-12** : `create module` accepte `--type` (défaut `EXTERNAL`) et
>   `--category` (défaut `SYSTEM`), validés contre le schéma.
> - **C/E-09..E-10** : `publish` envoie `token`/`description`/`icon`/`secondaryCategory`,
>   utilise `manifest.Category` (repli `SYSTEM`) comme catégorie primaire et
>   incrémente `buildNumber` (dernière build + 1) ; mock E2E idempotent par token.
> - **D/E-11** : l'audit/`Validator` contrôle `optionalRequirements`, `platforms`
>   (+`modes` si `supported`), les plages de compatibilité, `capabilities`,
>   `category` et `publisher` (WARNING par défaut).
> - **E/E-13** : `$schema` fixé au chemin SDK réel ; le rafraîchissement de session
>   privilégie `grant_type=refresh_token` (`/oauth/token`, rotation) quand un
>   refresh token OAuth est présent, avec repli sur `/api/auth/sessions/refresh`.
>
> Itération du 2026-09-16 (quater) : **`debug` pas-à-pas et sortie temps réel (v0.8.0)** —
> `liora debug` rapporte désormais chaque étape au fil de sa complétion (validation,
> détection du gestionnaire de paquets, résolution de la commande de build, exécution) et
> clôt l'exécution par un **récapitulatif de sévérité** (succès, notice, avertissement,
> erreur, obsolète) :
> - nouveau vocabulaire d'étapes partagé (`internal/tui/step.go`) : statuts, mise à jour en
>   place d'une étape identifiée et rendu live du tail de sortie (`RunWithSteps`, `Summarize`,
>   `SummaryBlock`) ;
> - la commande de build diffuse son `stdout`/`stderr` ligne par ligne sous une étape
>   `RUNNING`, puis bascule vers un statut terminal (fini le `CombinedOutput` qui tamponnait
>   tout et bloquait indéfiniment sur un serveur dev) ;
> - **fenêtres d'exécution** : un script `debug`/`dev` est arrêté après une fenêtre de
>   démarrage (15 s) et signalé démarré ; un build one-shot est plafonné (5 min) — `--timeout`
>   ajuste les deux (`internal/debug/debugger.go`) ;
> - **annulation** `Ctrl+C`/`Esc` (ou `SIGINT` quand non-interactif) : arrêt de l'arbre de
>   process (`proc_unix.go`/`proc_windows.go`, groupe dédié, SIGINT puis SIGKILL), carte de
>   confirmation et code de sortie `130` (`pkg.ExitCancelled`) ;
> - réconcilié avec le **build réel** déjà introduit (repli bundler `esbuild`/`tsup` puis
>   `tsc --noEmit`, v0.5.0).
>
> Itération du 2026-09-19 : **tests hermétiques (suite verte sur machine avec esbuild/tsup globaux)** —
> le repli bundler par PATH (spec §5.9) peut résoudre des bundlers installés globalement sur la
> machine du développeur et fausser la résolution `debug` du build :
> - `e2e/e2e_test.go` : le PATH des scénarios est désormais **hermétique** — fixtures du harnais
>   (`bun/npm/tsc/node`) puis uniquement `/usr/bin` et `/bin` ; le PATH de la machine n'est plus
>   hérité, aucun outil global (esbuild, tsup, tsc, vitest…) ne peut plus fuir dans les scénarios ;
> - `internal/debug/debugger_test.go` : helper `isolateToolchain` (PATH réduit à un répertoire vide)
>   posé sur les tests qui exécutent `DebugModule`/`DebugAll` ou la résolution bundler sans shim
>   explicite — le solveur ne retombe plus sur l'esbuild/tsup global de la machine ;
> - gofmt réaligné (`internal/config/paths.go`, `internal/signing/signer.go`, `cmd/init_test.go`).
>
> Itération du 2026-09-19 (bis) : **réduction des écarts restants (spec §5.6 étape 1, NFR-005)** —
> les deux derniers écarts constatés du rapport (§5, items `link`/`audit` « ✅/partiel » — en fait
> entièrement implémentés) sont clos :
> - **`publish` auto-connect (spec §5.6 étape 1)** : `cmd/publish.go` ne renvoie plus une erreur
>   quand l'utilisateur n'est pas connecté — il exécute d'abord le flux `liora connect`
>   (factorisé dans `cmd/connect.go` `doConnect()`, réutilisé par la commande `connect`), puis
>   recharge la session et poursuit la publication. Si l'auto-connexion échoue (ex. CI sans
>   `LIORIAN_CLI_CONNECT_*`), l'erreur catégorisée avec le fix `Run 'liora connect' first.`
>   est conservée (exit 2). Le scénario E2E TC-011 publie désormais **sans connexion préalable**
>   (le flux auto est vérifié), clé i18n `publish.info.connect` ajoutée (en-US/fr-FR, 396 clés).
> - **`debug.verbose` / `debug.logLevel` consommés (spec §6.1, NFR-005)** : `debugf`
>   (`cmd/root.go`) honore désormais la section `debug` de `lorian.config.json` — `debug.verbose:
>   true` **ou** `debug.logLevel: "debug"` active les logs verbose (résolus une fois par processus
>   depuis la racine projet, cache `sync.Once`), en plus de `--verbose` et `LIORIAN_CLI_DEBUG`. Les
>   deux champs du modèle `DebugConfig` n'étaient parsés que pour rien ; ils sont maintenant effectifs.
>
> Itération du 2026-09-20 : **clôture du dernier écart spec restant (spec §5.2 step 6)** —
> le drapeau `--skip-install` documenté par la spec et le rapport n'existait pas dans `create
> module` (seule la variable d'environnement `LIORIAN_CLI_SKIP_INSTALL=1` était honorée) :
> - `cmd/create.go` : flag `--skip-install` ajouté (désactive l'étape d'installation des
>   dépendances avec ou sans l'environnement), localisé `create.flag.skip_install` (en-US/fr-FR,
>   397 clés), registré via `i18nFlag` ; la garde devient
>   `if !createSkipInstall && os.Getenv(createSkipInstallEnv) == ""` ;
> - `cmd/create_test.go` : `TestRunCreateSkipInstallFlagSkipsInstallStep` (PATH vide → aucun
>   « no package manager detected » en stderr lorsque le flag est posé sans l'environnement) et
>   la liste des flags vérifiés par `TestCreateCommandFlagsRegistered` étendue (`type`,
>   `category`, `skip-install`) ;
> - suite complète verte : `go build`, `go vet`, `gofmt -l`, `go test ./...` (unitaires +
>   E2E testscript), `go test ./e2e/ -run TestScripts -count=1`.
>
> Itération du 2026-09-26 : **alignement sur la chaîne d'installation `module-installation`
> (specs workspace 0.49.0–0.50.0 `EXÉCUTÉ`)** — la CLI devient le premier maillon côté
> éditeur (lot G de `docs/plan-mise-a-niveau.md`) :
> - **Manifeste canonique** (`internal/module/manifest.go`) : `compatibility.{socle,api}`,
>   `oauth.scopes`, `capabilities: []string` (objet legacy normalisé à la lecture),
>   `external`, `dataModel` + `declarative` (§7.5 : pages `kpi`/`datagrid`/`form`/`chart`/
>   `settings`, widgets), enum `ModuleType` à 10 valeurs (legacy `INTERNAL`/`EXTERNAL`
>   acceptés, WARNING), permissions `Role:Verbe` (`IsPermissionCode`, catalogue 14 rôles ×
>   4 verbes), domaine canonique `mod.*` (`IsCanonicalDomain`, WARNING sinon) ;
>   `NewManifest` canonique (`WEB_APP_LOCAL`, compat `0.17.x`/`0.27.x`, `User:Get`…) ;
>   mockup = miroir 1:1 du socle ; `create` défaut `WEB_APP_LOCAL`.
> - **Pack `.liozip`** (`config`, `packer.go`, `cmd/pack.go`) : extension canonique ADR-003
>   (legacy lu, jamais produit), `manifest.json` canonique à la racine (TECH-002), quotas
>   NFR-006..008 (50 Mo, 5 000 entrées, ratio 100:1, 10 Mo/fichier, symlinks/exécutables
>   refusés), empreintes SHA-256 (archive + manifeste) exposées (`PackResult`) et affichées ;
>   flags `--out`/`--version`, args normalisés (`./x`, `library/modules/x`).
> - **Signature canonique obligatoire** (`signing`, `cmd/sign.go`, `cmd/publish.go`) :
>   charge utile `{moduleIdentifier, version, checksum, manifestChecksum, entry, type}`
>   (§7.1), `sign [module|archive] [--key fichier]` (manifeste lu à la racine de
>   l'archive), `verify` canonique + repli legacy signalé ; `publish` refuse sans clé
>   (ADR-010) sauf `--allow-unsigned`, déclare `manifestChecksum` + `signatureKeyId`
>   (fingerprint) ; flags `--file`/`--version` (session §16).
> - **Contrat version** (`store/publisher.go`) : compat effective + runtimes canoniques
>   (`web`, `tauri-desktop-*`, `tauri-mobile-*`) envoyés à la création de version.
> - **Install fail-closed** (`catalog/install.go`) : checksum absent → erreur, signature
>   absente/invérifiable → erreur (`--allow-unsigned` en dev), audit d'archive bloquant
>   (§7.2) ; mock E2E canonique (artefacts `.liozip`, manifeste racine).
> - **Validateur** : `compatibility`/`oauth.scopes`/`capabilities`/`permissions`/`type`/
>   domaine canonique/`CONFIGURATION` (`entry: index.json`, `dataModel`) en WARNING,
>   `index.tsx` exigé hors `CONFIGURATION` ; `publisher` absent = OK (schéma), incomplet =
>   WARNING ; `repair` aligné (compat canonique, `core:default`).
> - **Docs/i18n/E2E** : `docs/specs/liora.md` (§5.2, §5.5, §5.6, §5.13, FR/SEC, session §16,
>   références specs), `README.md`, ~30 clés i18n fr/en, scénarios `03/04/05/08/10/13/16`.

### 4.1 Sécurité — ✅ corrigé à l'itération du 2026-09-12
- **Fallback keychain → fichier chiffré activé** : `auth.NewStore()` et
  `signing.NewKeyStore()` sondent désormais le keychain OS (`keychainAvailable`,
  lecture d'une clé sentinelle) ; s'il est indisponible, elles basculent
  réellement sur le vault chiffré `~/.lorian-cli/credentials.enc` /
  `signing.enc` (spec §7.6, R-002).
- **Passphrases dures supprimées** : les AES utilisaient `"lior-cli-fallback-v1"` /
  `"lorian-cli-signing-v1"`. Désormais le secret est **aléatoire** (32 octets,
  `pkg.MachineSecret` → `~/.lorian-cli/machine.secret`, 0600) et la clé AES est
  **dérivée par PBKDF2-HMAC-SHA256** (210 000 itérations, sel par message,
  `pkg.EncryptVault`/`DecryptVault`). Aucun secret en dur dans le binaire.
- **Update désactivable en CI** : `LIORIAN_CLI_SKIP_UPDATE` (ou `CI` posée sans
  opt-in `LIORIAN_CLI_UPDATE`) coupe l'appel réseau (`pkg.update.skipUpdate`).
- **Erreurs du challenge MFA remontées** : `mfa.go` ne masque plus un
  `/api/mfa/challenge` en panne — l'erreur est rapportée avec le détail de la
  vérification.
- `NewStoreVolatile` = fallback isolé dans `/tmp` (secret aléatoire, ne pollue
  plus `~/.lorian-cli` en test) ; `NewKeyStoreVolatile` (mort) supprimé.
- **Rotation automatique du token (SEC-003, 🔧 renforcé au 2026-09-16)** :
  `pkg.Client.Do` retente les requêtes en échec HTTP 401 avec un token
  rafraîchi via `POST /api/auth/sessions/refresh` (`TokenRefreshFunc`, un seul
  retry pour éviter les boucles) ; `store.Client.WithAutoRefresh(sess)` câble
  le rafraîchissement sur `publish`, `link` et `unlink --sync-remote`. Les
  sessions longue durée ne replongent plus en erreur 401 sans reconnexion.

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
  `.lorian/links.json` (nom → token distant) ; la distinction local/distant
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

### Release 0.2.0 (Store) — ✅ faite
`publish` ✅ (dont conflit SemVer + PUT, auto-connect §5.6) · `link` ✅ · `unlink` ✅ · `audit` ✅ ·
`debug` ✅ (vrai build + trace pas-à-pas v0.8.0) · `sign keygen/sign/verify` ✅

### Release 0.3.0 (Qualité) — ✅ largement faite
- S-013 mode verbose/logs ✅ déjà présent (`--verbose`, `LIORIAN_CLI_DEBUG`).
- S-014 config `lorian.config.json` ✅ déjà présente.
- S-015 auto-update ✅ **complet au 2026-09-20** (notification seule, **pas de téléchargement
  automatique** de la nouvelle version, désactivable en CI). Correctif : la comparaison de version
  était inversée (`isNewer` faisait retomber sur vide — la notification ne s'affichait jamais) ;
  le check est désormais testable et testé de bout en bout (`LIORIAN_CLI_UPDATE_URL`, unitaires
  mockées + test E2E `TestUpdateNotification`, S-015 « jamais plus d'une requête GET »).
- S-016/017/018 tests unitaires + E2E (testscript) + CI — unitaires ✅ (15 packages), **E2E ✅** (13 scénarios
  txtar, TC-001 → TC-029, mock `liorian-connect` in-memory), **CI ✅** (job `e2e`).

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
5. **`debug` env.** — ✅ fait : script du `package.json` du module (parsing JSON, repli racine),
   repli bundler `esbuild`/`tsup` puis `tsc --noEmit`, plus de faux « OK » sans build réel ; puis
   **trace pas-à-pas + streaming + timeout + annulation** (v0.8.0, `internal/tui/step.go`).
6. **Candidats restants** :
   - testscript E2E (S-017) + CI sur scénarios TC-001 → TC-029 — ✅ fait : `e2e/` (mock
     `liorian-connect` in-memory, 13 scénarios `01_help_version` → `12_test` couvrant
     TC-001 → TC-029, fixtures `bun/npm/tsc/node/esbuild`, job CI `e2e`) ;
   - `disconnect`/`unlink` : option de mise à jour distante via `PUT /api/developer-store/modules/:id` (✅
     `unlink --sync-remote` couvert par TC-014) ;
   - **token refresh auto (SEC-003)** — ✅ fait au 2026-09-16 : retry 401 avec rotation du bearer
     token (`pkg.Client.TokenRefreshFunc` + `store.Client.WithAutoRefresh`) sur `publish`/`link`/
     `unlink --sync-remote` ;
   - **audit `domain` conforme spec §5.10** — ✅ fait au 2026-09-16 : format `mod.liorian.<name>`
     vérifié (WARNING), plus de simple reverse-DNS ;
   - **bâtir un vrai build de module dans `debug`** — ✅ fait au 2026-09-16 : sans script
     `debug/dev/build`, `debug` tente un **bundle réel** via un bundler résolvable
     (`esbuild`/`tsup`, node_modules module → racine → PATH) qui compile l'entrée dans `dist/`,
avant le repli `tsc --noEmit` puis `WARNING`. Tests unitaires + scénario E2E (fixture
   `esbuild`).
- **`liora test <module>`** — ✅ fait au 2026-09-16 (v0.9.0) : commande Cobra `test`,
      package `internal/moduletest` (validation + résolution script/runner + streaming + plafond),
      exit code **13** (échec de tests), extraction du runner partagé `internal/runner`, i18n
      `en-US`/`fr-FR`, tests unitaires + scénario E2E `12_test.txtar` (TC-028/TC-029). **Rehaussé en
      v0.10.0** : gestionnaire de paquets choisi à l'installation + catalogue (vitest/jest/mocha/ava)
      + installation dans le périmètre du gestionnaire + sélection interactive + persistance de la
      section `test` de `lorian.config.json` (cf. §2.4).
7. **Future spec** : `liora watch` (hot-reload), `liora deploy`,
   `liora marketplace` (§2.4 future scope) — `liora auth` (OAuth2 PKCE) ✅ fait au
   2026-09-16.

---

## 6. Commandes utiles

```bash
go build -o liora .
./liora --help
go test ./...              # unitaires + E2E testscript (TC-001 → TC-029)
go test ./e2e/ -run TestScripts -v   # suite E2E seule
go vet ./...
goreleaser release --clean   # release multi-plateforme
```

Couverture de test : unitaires ✅ (15 packages ok) + **E2E ✅** (`e2e/` : `TestMain` construit la CLI
depuis la racine repo, mock `liorian-connect` in-memory dans `e2e/mockapi/`, 13 scripts txtar
`e2e/testdata/scripts/01_help_version.txtar` → `12_test.txtar` couvrant TC-001 → TC-029, fixtures
exécutables `e2e/testdata/fixtures/bin/{bun,npm,tsc,node,esbuild}`, job CI `e2e`).
