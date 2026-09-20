# Changelog

All notable changes to this project will be documented in this file.


## [v0.17.0] - 2026-09-20

### Added
- **Assistant de création `create module` durci (spec §5.2)** — le parcours interactif est
  désormais validé étape par étape :
  - Les deux premières questions (domaine puis identifiant) sont fusionnées en une seule :
    la saisie de l'identifiant reverse-DNS (`com.organization.domain`) suffit et l'identifiant
    kebab-case est déduit en remplaçant les points par des tirets (`com.example.blog-manager` →
    `com-example-blog-manager`).
  - Chaque réponse est validée à la volée (`ValidateDomain`, `ValidateName`, `ValidateVersion`,
    `ValidateIcon`) et re-posée tant qu'elle est invalide : une erreur de saisie ne permet plus
    de passer à l'étape suivante.
  - Les suggestions sont dérivées de l'identifiant : nom d'application par défaut (dernier label
    du domaine) et URL de page proposée (`com.org.test` → `/org/test`).
- **`Tab` to fill** — dans les saisies textuelles interactives, la touche `Tab` accepte le
  placeholder comme réponse (hors champs secrets) : accepter la suggestion en une seule frappe
  au lieu de la retaper. Un indice `[tab : remplir]` est affiché sur les champs concernés.
- **Barres de progression thématiques** — nouveau helper `Styles.ProgressBar` /
  `Styles.ProgressLine` (remplissage accent sur piste douce, pourcentage rendu une seule fois,
  largeur adaptée au terminal et bornée entre 20 et 50), désormais utilisé par `RunWithProgress` :
  le look de la progression est unifié sur l'ensemble du CLI.

### Changed
- **Menus de sélection ajustés au terminal** — la question reste toujours visible en tête
  d'écran : la liste reserve la hauteur du titre et de la barre de navigation, son en-tête et sa
  pagination génériques sont masqués, et aucune option n'est plus rognée sur les petits
  terminaux. Les questions des invites (saisie, sélection, confirmation) partagent le même style.
- **Version bump** — `app.config.json` et le wrapper npm passent sur `0.17.0`.

### Technical Details
- Réorganisation de `collectCreateSpec` (validation par étape, extraction de `askValidated`),
  extraction de `newSelectModel`, et suppression du style `DimTitle` au profit de `Question`.

## [v0.15.0] - 2026-09-21

### Added
- **`marketplace search` / `marketplace install` (spec §2.4, §5.8, FR-001 storefront)** — le
  catalogue public est enfin consultable et installable en ligne de commande :
  - `liorian marketplace search [query]` interroge le storefront (`/api/catalog/*`), avec filtres
    `--category`, `--publisher`, `--module` et pagination (`--limit`/`--offset`), et affiche un
    tableau TUI (Module / Version / Catégorie / Éditeur). Une recherche sans correspondance est
    un résultat vide neutre, pas une erreur.
  - `liorian marketplace install <slug>` télécharge l'archive `.SenMod`, vérifie son **checksum
    SHA-256** (refuse la corruption, avec hint « retry the installation ») et sa **signature
    Ed25519** quand le catalogue en fournit une (`✓ Signature verified`), puis extrait le module
    de façon sûre dans `external_modules/`. Refuse les modules inconnus (exit 3), les doublons
    (`already installed`, exit 3, avec proposition `--force`/`--replace`), et rejette les
    archives non conformes (chemins `..`, absolus, incomplets).
  - Nouveau draft `internal/catalog` : client du storefront + moteur d'installation réutilisable
    (`catalog.CatalogModule`, `Installer`), avec gestion des erreurs i18n et des codes de sortie.
- **Garde-fou checksum e2e** — le harnais mock API seed désormais un catalogue déterministe
  (module signé Ed25519, module non signé, module « corrompu » dont le checksum catalogue ne
  correspond pas à l'artefact) pour couvrir le flux de refus sans casser les autres scénarios.
- **Scénario E2E `13_marketplace.txtar`** — recherche (résultats, filtres, résultat vide),
  installation signée et non signée, refus de doublon, `--force`, checksum invalide refusé,
  module inconnu (exit 3), et installation hors workspace refusée.

### Changed
- **Version bump** — `app.config.json` et le wrapper npm passent sur `0.15.0` (release
  anti-chronologique ; les travaux documentés en `v0.14.0` ci-dessous restent l'historique de la
  branche).

### Docs
- `docs/rapport-implementation.md` et `docs/specs/liorian.md` alignés (version courante du code
  `v0.15.0`, `marketplace` documenté comme implémenté).

## [v0.14.0] - 2026-09-20

### Added
- **`create module --skip-install`** — le drapeau documenté par la spec §5.2 (étape 6) existe
  enfin dans la commande : il désactive l'étape d'installation des dépendances du module, en plus
  de la variable d'environnement `LIORIAN_CLI_SKIP_INSTALL=1` (l'un ou l'autre suffit).
- **`publish` auto-connect (spec §5.6 étape 1)** — quand l'utilisateur n'est pas connecté,
  `liorian publish` exécute d'abord le flux `liorian connect` (factorisé dans `doConnect()`)
  puis poursuit la publication ; si l'auto-connexion échoue, l'erreur catégorisée
  (`Run 'liorian connect' first.`, exit 2) est conservée.

### Changed
- **`debug.verbose` / `debug.logLevel` effectifs (spec §6.1, NFR-005)** — `debugf` honore
  désormais la section `debug` de `lorian.config.json` (`debug.verbose: true` ou
  `debug.logLevel: "debug"`) en plus de `--verbose` et `LIORIAN_CLI_DEBUG`.
- **Tests hermétiques** — le PATH des scénarios E2E (et les tests `internal/debug`) est réduit
  aux fixtures du harnais puis `/usr/bin` et `/bin` : les outils globaux de la machine
  (esbuild, tsup, tsc, vitest…) ne peuvent plus fausser la résolution du build `debug`.
- **Version bump** — `app.config.json` et le wrapper npm sont sur `0.14.0`.

### Docs
- `docs/rapport-implementation.md` (version courante `v0.14.0`, itération du 2026-09-20) et
  `docs/specs/liorian.md` (métadonnées, dernière release documentée `0.14.0`) alignés.

## [v0.13.0] - 2026-09-19

### Changed
- **Nom commercial `Lior` / code de projet `lorian`** — le produit reste commandé `liorian`, mais ses surfaces publiques sont réparties
  en trois strates : **nom commercial `Lior`** (module Go `github.com/protorians/liorian-cli` → `github.com/protorians/lior-cli`,
  paquet npm `@liorian/cli` → `@lior/cli`, artefacts de release `liorian-cli_*` → `lior-cli_*`, client OAuth `lior-cli`) ;
  **code de projet `lorian`** (fichiers `liorian.config.json`/`liorian.config.toml` → `lorian.config.*`, schéma
  `liorian.config.schema.json` → `lorian.config.schema.json`, dossier projet `.liorian/` → `.lorian/`, vault `~/.liorian-cli` →
  `~/.lorian-cli`, services keychain `liorian-cli`/`liorian-cli-signing` → `lorian-cli`/`lorian-cli-signing`) ;
  **commande `liorian`** inchangée (binaire, wrapper npm, aide `Lior CLI`, variables `LIORIAN_*`).
- **L'écosystème reste `Liorian`** — applications plateforme (`Liorian Socle/Connect/Console/Store/Auth`), domaines
  `*.liorian.protorians.com`, template `protorians/liorian-socle`, domaine d'audit `mod.liorian.*` et SDK `@liorian/sdk`
  ne changent pas de nom.
- **Rupture** — les fichiers de configuration et chemins internes `liorian*` sont renommés `lorian*` ; les scripts et CI
  doivent être mis à jour. Bump `v0.13.0` (ligne 0.x).

## [v0.12.0] - 2026-09-18

### Changed
- **Changement de nom — `liorian`** — tout l'écosystème est renommé de `sentients`/`sentient` vers `liorian` : binaire et commande
  `sentients` → `liorian`, module Go `github.com/protorians/sentient-cli` → `github.com/protorians/liorian-cli`, paquet npm `@sentients/cli` →
  `@liorian/cli`, template de démarrage `protorians/sentients-socle` → `protorians/liorian-socle` et services de plateforme
  (`sentient-socle`, `sentient-connect`, `sentient-auth`, `sentient-store`) ainsi que domaines `*.sentient.protorians.com` →
  `*.liorian.protorians.com`.
- **Variables d'environnement renommées** — préfixe `SENTIENT_*` / `SENTIENTS` → `LIORIAN_*` / `$LIORIAN` (`LIORIAN_CLI_*`,
  `LIORIAN_AUTH_API`, `LIORIAN_CLI_TEMPLATE_REPO`, …).
- **Configuration et chemins** — `sentients.config.json` → `liorian.config.json`, schéma `sentient.config.schema.json` →
  `liorian.config.schema.json`, dossier projet `.sentients/` → `.liorian/`, vault et keychain locaux `~/.sentient-cli` →
  `~/.liorian-cli` (service `liorian-cli-signing`), et domaine d'audit `mod.sentients.<name>` → `mod.liorian.<name>`.
- **Rupture** — renommage global incompatible : les commandes, variables d'environnement, fichiers de configuration et chemins `sentient*`
  sont remplacés par `liorian*` ; mettez à jour vos scripts d'intégration et vos configurations. Bump `v0.12.0` (ligne 0.x).

## [v0.11.0] - 2026-09-18

### Added
- **Dossiers de test conventionnels détectés** — `liorian test` reconnaît désormais un module
  comme testable dès qu'il possède un dossier de test conventionnel (`__tests__/`, `__test__/`,
  `test/`, `tests/`, `spec/`, `specs/`), même lorsque ses fichiers n'ont pas le nommage
  `*.test.*` / `*.spec.*`. Ces modules ne sont plus faussement ignorés (`SKIPPED`) : la commande est
  bien lancée, ce qui élimine les faux `WARNING`/`ERROR` « No test files found ». Les dossiers
  `node_modules` et `.git` restent exclus de la détection.

### Changed
- **Spec `test` alignée** — `docs/specs/liorian.md` §5.15 (détection des fichiers **ou dossiers**
  de test) et `docs/rapport-implementation.md` synchronisés.

## [v0.10.0] - 2026-09-16

### Added
- **`liorian test` piloté par le gestionnaire de paquets choisi à l'installation** — la commande
  lit `project.packageManager` (`liorian.config.json`, écrit par `liorian init`), puis une
  surcharge `test.packageManager`, avant la détection PATH. Le package de test est résolu par ordre
  de priorité : flag `--runner`, config (`test.runner` / `test.modules.<domaine>.runner` avec les
  sentinelles `script` et `builtin`), script `test` du `package.json`, catalogue principal installé
  (`vitest`, `jest`, `mocha`, `ava`), puis runner intégré (`bun test`). Un package configuré mais
  absent est **installé en dépendance de développement** dans le périmètre du gestionnaire
  (`bun add -d`, `pnpm`/`yarn add -D`, `npm install -D`). Nouveau catalogue
  `internal/moduletest/catalog.go` et helpers `pkg.DevDependencyArgs`.
- **Sélection interactive du package de test** — lorsqu'aucun package n'est configuré, installé ou
  intégré et que le module contient des tests, le développeur choisit parmi les packages principaux
  (avec l'état d'installation) ou saisit un package personnalisé, puis valide son installation. Le
  choix est effectué **avant** la trace pas-à-pas (aucune collision entre programmes Bubble Tea).
- **Persistance dans `liorian.config.json`** — nouveau bloc `test` (`packageManager`, `runner`,
  `modules.<domaine>.runner`) avec `TestConfig.RunnerFor`/`SetRunner` ; le package de test résolu et
  le gestionnaire réellement utilisé y sont écrits automatiquement.
- **Modules sans fichier de test ignorés** — un module dépourvu de fichier de test (`*.test.*`,
  `*.spec.*`, `__tests__/`) est **ignoré** (statut `SKIPPED`, étape `NOTICE`) même si un script ou un
  runner est configuré : la commande n'est pas lancée, ce qui évite l'échec « No test files found »
  et le faux `ERROR` du runner.

### Changed
- **Spec `test` alignée** — `docs/specs/liorian.md` §5.15 (résolution du package de test, sélection
  interactive, persistance, flag `--runner`, sortie TUI) et §6.1 (bloc `test` documenté).

## [v0.9.0] - 2026-09-16

### Added
- **`liorian test [module]` — exécution des tests (spec §2.4, premier item du future scope)** —
  nouveau package `internal/moduletest` (`Tester`, `TestResult`) : validation du module + détection
  du gestionnaire de paquets + résolution de la commande de test (script `test` du `package.json` du
  module puis du projet, repli sur un runner réel `vitest run`/`jest --ci --runInBand`/`bun test`
  uniquement si le module contient des fichiers de test, sinon `WARNING` — jamais un faux « OK »).
  Exécution en streaming live (tail 8 lignes) avec plafond 2 min par défaut (`--timeout`), annulation
  `Ctrl+C`/`Esc`/`SIGINT` (exit `130`) et récapitulatif de sévérité. **Exit code `13`** (échec de
  tests, spec §11.1) : le run échoue dès qu'un module a un statut `ERROR`. Mode all modules :
  tableau + logs + récapitulatif global. i18n `en-US`/`fr-FR`, tests unitaires
  (`internal/moduletest`, 9 tests) et scénario E2E `12_test` (TC-028/TC-029).
- **Runner partagé `internal/runner`** — extraction du streaming `stdout`/`stderr` avec groupe de
  process dédié, fenêtre de démarrage, plafond d'exécution et arrêt SIGINT→SIGKILL depuis
  `internal/debug` (`runStream`/`scanLines`/proc) vers `internal/runner` (`Run`, `Outcome`). `debug`
  est refactoré sur ce runner ; couvert par des tests unitaires.

### Changed
- **Spec alignée** — `docs/specs/liorian.md` : `liorian test` déplacé du périmètre futur au
  périmètre, nouvelle section §5.15 (Comportement, Flags, Vocabulaire d'étapes, Sorties TUI),
  code de sortie `13` ajouté au §11.1, scénarios TC-028/TC-029 ajoutés au §12 ; §2 répertoire des
  commandes et §4.1 mis à jour (packages `moduletest`, `runner`). `docs/rapport-implementation.md`,
  `README.md` et `CHANGELOG.md` synchronisés.

## [v0.8.1] - 2026-09-16

### Docs
- **Spec `debug` alignée sur le code (v0.8.0)** — `docs/specs/liorian.md` §5.9 réécrite : trace pas-à-pas des étapes (`RUNNING` → statut terminal), sortie de build diffusée en temps réel (tail 8 lignes), fenêtres d'exécution (`--timeout`, script dev 15 s / build one-shot 5 min), annulation `Ctrl+C`/`Esc`/`SIGINT` (exit `130`) et récapitulatif de sévérité ; flag `--timeout`, vocabulaire d'étapes partagé (`internal/tui/step.go`) et arborescence §4.1 (`proc_unix.go`/`proc_windows.go`/`step.go`) documentés. `docs/rapport-implementation.md` synchronisé (version courante, itération du 2026-09-16 « quater », compteurs packages/tests et scénarios E2E TC-001 → TC-027).


## [v0.8.0] - 2026-09-16

### Added
- **`debug` pas-à-pas et résumé de sévérité** — `liorian debug [module]` rapporte désormais chaque étape (validation, détection du gestionnaire de paquets, résolution de la commande de build, exécution) au fur et à mesure de sa complétion, et clôt l'exécution par un récapitulatif par sévérité (succès, notice, avertissement, erreur, obsolète). Les findings de validation et les replis (bundler, `tsc`) sont comptés distinctement.
- **Sortie de build en temps réel** — les commandes de build diffusent leur `stdout`/`stderr` ligne par ligne sous une étape « en cours » (`RUNNING`), qui montre la sous-tâche active et sa queue de sortie avant de basculer vers un statut terminal.
- **Fenêtre d'exécution `--timeout`** — un script `debug`/`dev` (serveur/watch qui ne se termine jamais) est arrêté après une fenêtre de démarrage (15 s par défaut) et signalé comme démarré ; un build one-shot est plafonné (5 min par défaut). `--timeout` ajuste les deux.
- **Annulation avec confirmation** — `Ctrl+C` (ou `Esc`, ou `SIGINT` en mode non interactif) annule le débogage : l'arbre de process du build est arrêté (SIGINT puis SIGKILL du groupe), une carte de confirmation est affichée et le code de sortie `130` est retourné.

### Changed
- **`debug` ne bloque plus sur un serveur dev** — les commandes étaient exécutées via `CombinedOutput` (sortie tamponnée, aucune information pendant l'exécution, blocage infini) ; elles s'exécutent désormais en streaming dans un groupe de process dédié (`internal/debug/proc_unix.go` / `proc_windows.go`).
- **Trace TUI** — introduction d'un vocabulaire d'étapes partagé (`internal/tui/step.go`) : statuts `RUNNING`/`SUCCESS`/`NOTICE`/`WARNING`/`ERROR`/`DEPRECATED`, mise à jour d'une étape en place par identifiant, et rendu live du tail de sortie.

## [v0.7.0] - 2026-09-16

### Added
- **Manifeste conforme au schéma canonique (plan de mise à niveau, lot A/D)** — `module.Manifest` modélise désormais l'intégralité du contrat `module-manifest.schema.json` : `$schema`, `optionalRequirements`, `logo`/`banner`, `category`, `configSettings`, capacités étendues (`requiresAdmin`, `supportsRealtime`, `processesLocalData`), métadonnées éditeur (`url`, `email`, `description`, `avatar`), items de menu enrichis (description, target, keywords, items, separator) et plateformes détaillées (`os`, `iosSupported`, `minOsVersion`). Un sac d'extensions `Extra` (`json.RawMessage`) garantit qu'aucun champ canonique inconnu n'est perdu au round-trip (`additionalProperties: true`).
- **`create module` : `--type` et `--category` (lot B)** — le type de distribution (`INTERNAL`/`EXTERNAL`, défaut `EXTERNAL` pour un module tiers) et la catégorie de store (enum `ModuleCategory`, défaut `SYSTEM`) sont validés puis écrits dans `manifest.json` et `index.tsx`.
- **Publication enrichie (lot C)** — `publish` envoie désormais `token`, `description`, `icon` et `secondaryCategory` à la création du produit et utilise `manifest.category` comme catégorie primaire (repli `SYSTEM`) ; `buildNumber` est incrémenté (dernière build + 1) au lieu d'être figé à `1`.
- **Audit étendu (lot D)** — le `Validator` contrôle en WARNING `optionalRequirements`, `platforms` (+ `modes` si `supported`), les plages `managerCompatibility`/`apiCompatibility` (max complète, ex. `0.17.x`), `capabilities`, `category` et `publisher`.
- **Rafraîchissement OAuth (lot E)** — une session issue de `liorian auth` se rafraîchit via `POST /oauth/token` (`grant_type=refresh_token`, rotation) quand un refresh token OAuth est présent, avec repli sur `POST /api/auth/sessions/refresh`.

### Changed
- **Mockup embarqué aligné sur le socle** — `hello-world/manifest.json` miroir 1:1 (ajout `$schema` pointant vers le SDK réel et `optionalRequirements: {}`) ; `index.tsx` ne déclare plus `requirements`/`dependencies`/`devDependencies` (le manifeste reste la source de vérité).
- **`NewManifest`** — défauts conformes (`optionalRequirements: {}`, `category: SYSTEM`, plages de compatibilité avec `min` seule, suppression du `max` invalide `*.x`).

## [v0.6.0] - 2026-09-16

### Added
- **`liorian auth` — OAuth2 authorization-code + PKCE (spec §2.4, future scope)** — authentifie le développeur via le navigateur : génère un code verifier + challenge S256 (RFC 7636) et un `state`, ouvre la page d'autorisation de `liorian-auth`, reçoit la redirection sur un serveur local en boucle (`127.0.0.1:<port>/callback`), échange le code au point d'entrée `tokenEndpoint`, puis stocke la session (access token + refresh token + expiration) dans le trousseau système. Les endpoints, le `clientId` et les `scopes` proviennent de l'entrée `oauth` de `liorian-auth` dans `app.config.json` (avec défauts). En mode non interactif (CI), le code est fourni via `LIORIAN_CLI_AUTH_CODE` (pas de navigateur ni de serveur local). Couvert par des tests unitaires (`internal/auth` : PKCE, URL d'autorisation, échange de code, stockage) et un scénario E2E (`11_auth`, mock `/oauth/token`).


## [v0.5.0] - 2026-09-16

### Added
- **`debug` : vrai build de module (spec §5.9)** — sans script `debug`/`dev`/`build` dans le `package.json`, `liorian debug` tente désormais un **bundle réel** via un bundler résolvable (`esbuild`, `tsup`) — node_modules du module → node_modules racine → PATH — compilant l'entrée du module dans `dist/` (sortie réelle, plus seulement un type-check). Ce n'est qu'à défaut de bundler qu'il retombe sur `tsc --noEmit`, puis sur un statut `WARNING`. Couvert par des tests unitaires (`internal/debug`) et un scénario E2E (fixture `esbuild`).


## [v0.4.1] - 2026-09-16

### Fixed
- **Rotation automatique du token (SEC-003)** — les appels API authentifiés (`publish`, `link`, `unlink --sync-remote`) détectent désormais les réponses HTTP 401 et rafraîchissent automatiquement le bearer token via `POST /api/auth/sessions/refresh` avant de retenter la requête une seule fois. Les sessions longue durée ne replongent plus en erreur 401 sans reconnexion (`pkg.Client.TokenRefreshFunc` + `store.Client.WithAutoRefresh`).
- **Audit `domain` conforme spec §5.10** — la règle `domain` du manifest vérifie désormais le format attendu `mod.liorian.<name>` (WARNING) au lieu d'accepter toute forme reverse-DNS valide. Les modules scaffolés (domaine `mod.liorian.<id>`) restent conformes.


## [v0.4.0] - 2026-09-16

### Added
- **`create module` interactif** — la création demande d'abord le **domaine** du module (forme `com.organization.domain`) puis l'**identifiant** (kebab-case, ex. `hello-world`), suivis du nom de l'application et d'informations optionnelles (version, icône lucide-react, url de la page, description). De nouveaux drapeaux `--domain`, `--id`, `--name`, `--version`, `--icon`, `--url` et `--description` permettent de tout fournir en non-interactif ; l'argument positionnel reste un raccourci pour l'identifiant.
- **Manifest** — l'identité du module scaffolé est complète : champs `id`, `domain`, `key`, `name`, `description`, `version`, `icon` et `uri` renseignés depuis le spec de création (défaut : version `0.0.0`, url de page = identifiant). Validations ajoutées : domaine reverse-DNS, version SemVer, icône PascalCase.
- **`pack <module>@<version>`** — construction d'une version précise du module via le séparateur `@` (ex. `com.example.blog-manager@2.1.0`) ; la version du manifeste reste utilisée sinon.
- **`pack`** — l'archive embarque désormais la **page déployée** (`src/app/<url>/` d'après le `uri` du manifest, repli sur l'identifiant) en plus du module et de ses assets.

### Changed
- **Archives `.SenMod` (changement cassant)** — les archives construites passent de `.smp` à `.SenMod` (`<module>-<version>.SenMod`) ; les fichiers de signature deviennent `<module>-<version>.SenMod.sig`. La constante `config.ArchiveExt` centralise l'extension.
- **`create module` par domaine** — le module est scaffolé sous `external_modules/<domain>/` (et `public/assets/<domain>/`) et adressé partout par son **domaine** (pack, sign, debug, audit, link) ; les modules requis peuvent aussi être résolus par leur `id` de manifest.
- **Audit** — la règle `domain` du manifest vérifie la forme reverse-DNS et s'assure que le répertoire du module porte son domaine.
- **Mockup hello-world** — `@liorian/sdk` passe de `workspace:*` à `latest`.

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
- **`create module` — installation des dépendances** : les `dependencies` et `devDependencies` du manifest sont résolues via le premier gestionnaire de paquets détecté (`bun → pnpm → yarn → npm`) à la racine du projet. Désactivable via `LIORIAN_CLI_SKIP_INSTALL=1` ; un échec d'installation ou l'absence de gestionnaire reste non-bloquant (simple avertissement).
- **Manifest** — prise en charge du champ `devDependencies`, ajouté au mockup embarqué hello-world.

### Changed
- **Audit** — la vérification des requirements réutilise le moteur de résolution commun à `create` (`external_modules/` **ou** `src/modules/`).

### Technical Details
- **CI / Release** — les workflows GitHub sont reconstruits de zéro : pipeline de release déclenché par un tag `vX.Y.Z` (créable aussi via `workflow_dispatch`), build des binaires GoReleaser Windows/macOS/Linux dans `./dist` (archives + binaires nus + `checksums.txt`), publication automatique de la release GitHub dont le sommaire contient les liens de téléchargement des binaires et les détails du changelog (`scripts/release-notes.sh`).

### Docs
- `docs/specs/liorian.md` §5.2 mis à jour (vérification des requirements et installation des dépendances dans le pipeline `create module`) ; `docs/rapport-implementation.md` complété.


## [v0.2.0] - 2026-09-15

### Added
- **`create module --mockup` / `--page-mockup`** — le module et la page sont scaffolés depuis des répertoires/gabarits explicites (priorité sur `LIORIAN_MODULE_MOCKUP` / `LIORIAN_PAGE_MOCKUP`), avec repli silencieux sur les mockups embarqués si la source est inutilisable.

### Changed
- **Mockup hello-world aligné 1:1 sur le socle** — le mockup embarqué est désormais identique au module de référence `liorian-socle/external_modules/hello-world` (API SDK `View.*` / `Activity.*`, `AutoBreadcrumb`) ; les imports obsolètes (`Wrapper`, `WaitingActivity`, `AnimatedContent`) sont retirés.

### Docs
- `docs/specs/liorian.md` réalignée sur le code (arborescence `domain/hello-world.interface.ts`, flags de surcharge des mockups, metadata rel. 0.2.0) ; `docs/rapport-implementation.md` et `README.md` mis à jour.


## [v0.1.0] - 2026-09-15

### Added
- **Internationalisation (i18n)** — interface bilingue `en-US` (défaut) / `fr-FR` : aide, descriptions, prompts, flags et erreurs sont traduits. La langue est résolue dans l'ordre `--lang` → `LIORIAN_CLI_LANG` → `cli.lang` de `liorian.config.json` → locale de l'OS (`LC_ALL`/`LC_MESSAGES`/`LANG`), avec repli sur `en-US`.
- **Registre d'applications embarqué** — `app.config.json` est compilé dans le binaire (`//go:embed`) : chaque commande résout l'URL de base et le timeout des API (`liorian-auth`, `liorian-store`) depuis le registre, surchargeable par un `app.config.json` local remonté des répertoires ou par `LIORIAN_AUTH_API`.
- **`create module` depuis le mockup hello-world** — le module est généré depuis un module de référence embarqué (Clean Architecture : `application/`, `domain/`, `infrastructure/`, `presentation/`, `manifest.json`, `index.tsx`), renommé avec le nom du module ; une page `src/app/<name>/page.tsx` est aussi scaffolée quand le manifest déclare une `uri`/`url`. Surcharges `LIORIAN_MODULE_MOCKUP` et `LIORIAN_PAGE_MOCKUP`.
- **`init` par canal de release** — téléchargement du ZIP de release du template `protorians/liorian-socle` (au lieu d'un clone git) avec `--channel alpha|beta|rc|stable` (stable par défaut) ; les dossiers existants non vides ne sont plus écrasés sans accord explicite.
- **`audit --output table|json`** — sortie machine (JSON) ou tableau pour les audits de modules.
- **`unlink --sync-remote`** — synchronisation des métadonnées locales (nom, type, description) vers le produit distant avant le déliage.
- **Scripts de développement** — `scripts/dev-uninstall.sh` ; `dev-install.sh` gagne `--build-only`, `--install-only` et `--prefix`.
- **TUI** — aide thématisée (wordmark, sections teintées) et composants/styles cohérents, avec repli `--no-color` stable pour les tests E2E.

### Changed
- **Authentification liorian-auth** — la base URL n'est plus codée en dur : résolution `LIORIAN_AUTH_API` → registre workspace → registre embarqué ; le champ `device` devient une chaîne ; timeouts configurés par application (`api.timeout`).
- **Configuration JSON** — `liorian.config.json` remplace le TOML (`.liorian-cli.toml`) avec des clés camelCase ; le parser TOML est retiré.
- **User-Agent standardisé** `Protorians/5.0 (…) Senteints/<version>` sur toutes les requêtes HTTP de la CLI et de l'installeur npm.
- **`pack`** inclut désormais `src/app/<module.url>/` dans l'archive `.SenMod`.

### Removed
- `scripts/release.sh` — le release passe par les tags git et les workflows CI/GoReleaser.

### Docs
- `docs/specs/liorian.md` réalignée sur le code (i18n FR-025/NFR-007, registre `app.config.json` TECH-009, FR-002/004/008/010/012/014/015) ; `docs/rapport-implementation.md` et `README.md` mis à jour (nouvelle config JSON, commandes et variables d'environnement).


## [v0.0.9] - 2026-09-12

### Changed
- **Alignement API sur les contrats documentés** (`liorian-workspace`) :
  - Enveloppe Raiton `{ message, data, statusCode }` décodée dans `internal/pkg/http.go` (rétro-compat `{code,message}`) ;
  - Auth **à jeton unique** : `POST /api/auth/sign-in` → `{user, token, device}`, `/api/auth/logout`,
    `/api/auth/sessions/refresh` ; le `refresh_token` est retiré du modèle de session ;
  - MFA **gardée** : `POST /api/mfa/challenge` puis `/api/mfa/totp/verify` ou `/api/mfa/recovery/verify`,
    exécutés avec la session après sign-in ;
  - Store sur l'API **developer-store** : `ListModules`/`GetModule`/`UpdateModule`/`Publish` passent par
    `/api/developer-store/modules/**` — la publication suit le pipeline produit → version → artefact
    (checksum SHA-256 hex + signature Ed25519 base64 du `.SenMod.sig` + poids).

### Docs
- `docs/specs/liorian.md` §5.3/5.4/5.6/5.7, §6.3 et §8 (endpoints + DTOs) réalignés sur les contrats documentés ;
- `docs/rapport-implementation.md` (# connect/disconnect/publish/link) mis à jour.


## [v0.0.8] - 2026-09-12

### Changed
- **Commande renommée `liorian` → `liorian`** : le binaire et toutes les invocations de la CLI utilisent désormais `liorian` (Cobra, GoReleaser, binaire npm, CI).

### Docs
- Mise à jour de la documentation et de la spécification (`README.md`, `docs/specs/liorian.md`, `docs/rapport-implementation.md`) avec la commande `liorian`.


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

- **init** — Cloner `protorians/liorian-socle` + installer les dépendances (détection bun/pnpm/yarn/npm)
- **create module** — Créer un module standardisé dans `external_modules/`
- **connect** — Authentification via liorian-connect (email + mot de passe, MFA TOTP / backup codes)
- **disconnect** — Invalider le token côté serveur et supprimer les credentials
- **pack** — Construire l'archive `.SenMod` dans `.liorian/build/`
- **sign keygen** — Générer une paire de clés Ed25519 pour la signature
- **sign [module]** — Signer l'archive `.SenMod` d'un module (Ed25519)
- **sign verify [module]** — Vérifier la signature d'un module
- **publish** — Auditer, packer et publier un module sur le store
- **link / unlink** — Associer un module local à un module distant du store (token)
- **audit** — Auditer la conformité (Clean Architecture, manifest, dépendances, assets)
- **debug** — Valider le module et lancer un build de diagnostic
- Internal packages: auth, config, module, pkg, tui, signing, audit, debug, store
- NPM wrapper package (`@liorian/cli`)
- GitHub Actions workflows (CI, release, version bump)
- AES-256-GCM encrypted fallback for signing keys (`~/.liorian-cli/signing.enc`)
- OS keychain integration for signing keys via `go-keyring`
