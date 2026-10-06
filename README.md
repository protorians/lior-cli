# Liora CLI

Outil de développement en ligne de commande pour créer, maintenir et publier
des modules dans l'écosystème Liora (spécification : `docs/specs/liora.md`).

Cycle de vie : `init → create → develop → debug → audit → pack → sign → publish`

## Stack

- Go (la spécification exige 1.22+ ; l'outillage Charm actuel requiert une
  toolchain récente — voir `go.mod` pour la version minimale exacte)
- [Cobra](https://github.com/spf13/cobra) — parsing de commandes
- [Bubble Tea](https://github.com/charmbracelet/bubbletea) + [Lipgloss](https://github.com/charmbracelet/lipgloss) + [Bubbles](https://github.com/charmbracelet/bubbles) — TUI
- [go-keyring](https://github.com/zalando/go-keyring) — credentials dans le keychain système
- Binaire unique multi-plateforme (GoReleaser)

## Installation (développement)

```bash
go build -o liora .
./liora --help
```

## Installation (distribution)

### Homebrew (macOS / Linux)

```bash
brew tap protorians/lior-cli https://github.com/protorians/lior-cli.git
brew install protorians/lior-cli/liora
```

> Homebrew exige une référence `user/repo/formula` (3 segments) : la forme
> `brew install protorians/lior-cli` (2 segments) n'est pas valide dans Homebrew.

### Go / npm / binaire

Voir la section « Installation » de la spécification (`docs/specs/liora.md`,
§10.2) pour les canaux `go install`, `npm`/`npx`, script `curl` et Windows.

## Commandes

| Commande | Description |
|----------|-------------|
| `liora init [--channel alpha\|beta\|rc\|stable] [--auto-env]` | Télécharger la release ZIP de `protorians/liorian-socle` (canal stable par défaut) + installer les dépendances (détection bun/pnpm/yarn/npm) ; génère le `.env` depuis l'exemple du template (clé applicative, paire VAPID, nom/slug) — `--auto-env` (ou `LIORIAN_CLI_ENV_AUTO`) accepte toutes les valeurs suggérées sans question |
| `liora create module [nom] [--domain d] [--id id] [--publisher slug] [--type TYPE] [--category CAT] [--mockup dir] [--page-mockup file] [--standalone]` | Créer un module dans `library/modules/` depuis le **mockup de référence adapté au type choisi** — un mockup embarqué par type canonique (spec `module-types` §2 : `configuration` → `settings.tsx` + pages déclaratives, `service` → `routines.tsx`, `widget` → widgets du dashboard, `theme` → palettes de tokens, `web-app-remote` → section `remote`, `hello-world` → application `WEB_APP_LOCAL` complète, `system` → console d'administration Tauri ; miroir du socle : `compatibility`, `oauth`, `capabilities`, `permissions` en domaines nus, `userScope`), surchargeable via `--mockup` / `--page-mockup` (`LIORIAN_MODULE_MOCKUP` / `LIORIAN_PAGE_MOCKUP`) ; `--type` (enum `ModuleType`, défaut `WEB_APP_LOCAL`) et `--category` (défaut `SYSTEM`) sont écrits dans le manifeste et la déclaration. **Le wizard interactif est piloté par la session** : le type est d'abord demandé (il décide du mockup scaffoldé **et** du préfixe canonique du domaine), puis le slug d'organisation est récupéré du compte connecté, puis le domaine inversé est proposé **pré-rempli** `<prefixe(type)>.<slug>.` — le développeur ne complète que l'identifiant, et l'identité entière est conservée pour la suite. Les types sans surface applicative (`CONFIGURATION`, `SERVICE`, `WIDGET`, `THEME`) et `WEB_APP_REMOTE` ne scaffoldent pas de page `src/app/<url>/`. Une organisation **sans slug** doit en définir un via la CLI avant toute création ; `--publisher` est l'échappatoire CI / rejeu de script. `--standalone` crée à la place un **dépôt de module autonome** dans le répertoire courant (racine `liorian.config.json`, `.gitignore`, `README.md`, module sous `modules/<id>/` sans page `src/app/` et `uri` préfixé `/m/<id>`) — sans exiger de projet existant ; un répertoire non vide ou imbriqué dans un autre projet est refusé |
| `liora create view <module> [nom] [--name id] [--label titre] [--description texte] [--mockup file]` | Créer une vue de présentation (`presentation/views/<id>.view.tsx`) dans un module existant depuis le mockup embarqué (composant `<Id>View`, titre et description renommés) |
| `liora connect` | Authentification via liorian-connect (email + mot de passe, MFA TOTP / backup codes) ; le récapitulatif affiche le **slug d'organisation** du compte connecté, et une organisation qui n'en expose aucun est invitée à le définir dans la foulée (c'est ce slug qui nomme ensuite vos modules dans `create module`) |
| `liora auth` | Authentification OAuth2 (code d'autorisation + PKCE) via le navigateur — endpoints issus de `app.config.json` (`oauth` de `liorian-auth`) ; mode CI via `LIORIAN_CLI_AUTH_CODE` |
| `liora disconnect` | Invalider le token côté serveur et supprimer les credentials |
| `liora artifact build\|dev\|pack\|typecheck\|test [module]` | Chaîne de développement d'un module, **native dans le binaire `liora`** (esbuild via son API Go, aucune dépendance Node) : `build` produit le bundle + le document hôte dans `.liorian/artifact/` (D7), `dev` sert ce layout avec un flux **SSE** `/-/events` qui recharge les iframes après chaque rebuild (`--port`, `--host`, `--https`/`--http`, `--strict-port`, `--socle <dir>` pour démarrer aussi le socle) ; **plusieurs modules** (`liora artifact dev crm billing`, `--all`) vivent dans un seul serveur, un seul port, chaque module servi sous son slug `/<slug>/` avec un reload SSE taggué par module, et le `.env.local` du socle lié aligné sur le lot, `pack` enchaîne build + validations D6/D16 + archive `.LiorArtifactPackage`, `typecheck` exécute `tsc --noEmit`, `test` lance le script `test` du module. Le module visé est celui qui porte le répertoire courant, à défaut celui nommé en argument, à défaut celui sélectionné depuis la racine du projet |
| `liora artifact bind:socle <socle> [module]` / `unbind:socle` | Lier un module en développement à un socle hors de son dossier : `library/modules/<id>/` est écrit en **liens symboliques** vers le module (repli copie) et le HMR est câblé dans le `.env.local` non versionné du socle. Le chemin du socle est absolutisé depuis le répertoire d'appel avant la résolution du module ; `unbind:socle` retire la liaison sans toucher une installation réelle |
| `liora pack [module[@version]] [--version V] [--out F]` | Construire l'archive `.LiorArtifactPackage` dans `.liorian/build/` — layout runtime isolé : manifeste canonique à la racine + `src/**` (source TS, D4) + `artifact/**` (payload exécutable, §4.4), typecheck bloquant (D6), `userScope` obligatoire (D15), ni `next/*` (D1) ni `fetch`/`WebSocket` (D16) dans le bundle ; quotas 50 Mo/5 000 fichiers/ratio 100:1, empreintes SHA-256 affichées |
| `liora sign keygen` | Générer une paire de clés Ed25519 pour la signature |
| `liora sign [module\|archive.LiorArtifactPackage] [--key F]` | Signer la charge utile canonique de publication (Ed25519, obligatoire pour publier — ADR-010) |
| `liora sign verify [module\|archive.LiorArtifactPackage]` | Vérifier la signature d'un module (une seule extension admise : `.LiorArtifactPackage`) |
| `liora publish [module] [--version V] [--file F] [--allow-unsigned]` | Auditer, packer, signer (obligatoire) et publier un module sur le store (`manifestChecksum` + `signatureKeyId` déclarés) |
| `liora link` / `unlink` | Associer un module local à un module distant du store (token) |
| `liora marketplace search\|install` | Rechercher (`search [query]`) et installer (`install <module>`) des modules depuis le catalogue public (`LIORIAN_STORE_API`) |
| `liora install <archive.LiorArtifactPackage> [--force]` | Installer une archive locale sans le marketplace ni api-core : audit fail-closed, installation multi-version dans `library/modules/<id>/<version>/` + pointeur `current` (D11) ; le sidecar `.sig` est vérifié contre le trousseau quand il existe |
| `liora audit [module]` | Auditer la conformité (Clean Architecture, manifest, dépendances) |
| `liora repair [module] [--dry-run] [--warnings] [--no-install] [--no-interaction] [--output table\|json]` | Réparer automatiquement les anomalies bloquantes d'un (ou tous les) module(s) : champs de manifeste, nom du dossier, dépendances npm manquantes, JSON malformé ; les points non réparables deviennent des instructions pas à pas |
| `liora debug [module]` | Valider le module et lancer un build de diagnostic |
| `liora doctor [--socle path] [-m module] [--fix] [--output table\|json]` | Diagnostiquer la boucle de développement d'un module — identité, `.env` du socle, certificats mkcert, transport et réponse effective du registre d'installation, liaison `.lierian/dev.json`, transport du dev-server, socle en écoute ; chaque contrôle non conforme affiche la commande qui le lève. `--fix` corrige sans risque ce qui peut l'être (`.env`, transport de la bibliothèque, certificats, re-liaison via `artifact bind:socle`). `--output json` sort en erreur dès qu'un contrôle échoue, pour une intégration CI |
| `liora test [module]` | Exécuter les tests via le gestionnaire choisi à l'installation (script `test`, vitest/jest, `bun test`) ; package de test persisté dans `liorian.config.json` ; exit `13` en cas d'échec |
| `liora dev [-- args]` | Démarrer le serveur de développement de l'application (script `dev`) via le gestionnaire choisi à l'installation (Ctrl+C pour arrêter) ; exécute d'abord l'`audit` des modules |
| `liora build [-- args]` | Construire l'application (script `build`) via le gestionnaire choisi à l'installation ; exécute d'abord l'`audit` des modules ; exit = code de la commande |
| `liora start [-- args]` | Démarrer l'application en production (script `start`) via le gestionnaire choisi à l'installation ; exécute d'abord l'`audit` des modules |
| `liora check [-- args]` | Exécuter l'analyse statique (script `lint` par défaut, remappable via `toolchain.commands.check`) ; exit = code de la commande |
| `liora -v` / `--version` | Afficher la version |
| `liora help` | Aide contextuelle |

> Une nouvelle version détectée au démarrage affiche une **notification seule**
> (`Update available: vX → vY`) — le CLI ne télécharge ni n'impose jamais la
> mise à jour (S-015 / NFR-006), et le check est désactivable via
> `LIORIAN_CLI_SKIP_UPDATE` (cache 24 h).

## Configuration

`liorian.config.json` (optionnel, à la racine du projet) :

```json
{
  "project": {
    "name": "mon-projet",
    "packageManager": "bun"
  },
  "publish": {
    "defaultRegistry": "https://store.liorian.dev",
    "autoAudit": true
  },
  "debug": {
    "verbose": false,
    "logLevel": "info"
  },
  "test": {
    "packageManager": "bun",
    "runner": "vitest",
    "modules": {
      "com.example.blog-manager": { "runner": "jest" }
    }
  },
  "toolchain": {
    "commands": { "check": "typecheck" },
    "before": { "build": ["pre-build"] },
    "after": { "build": ["post-build"] }
  }
}
```

> Le bloc `test` est écrit automatiquement par `liora test` : `packageManager` reprend le
> gestionnaire choisi à l'installation, `runner` le package de test par défaut et
> `modules.<domaine>.runner` une surcharge par module (`vitest`, `jest`, `mocha`, `ava`, un package
> personnalisé, `builtin` ou `script`).
>
> Le bloc `toolchain` pilote `liora dev/build/start/check` : `commands` remappe la commande vers un
> script `package.json` (défauts : `dev`→`dev`, `build`→`build`, `start`→`start`, `check`→`lint`),
> `before`/`after` listent les scripts exécutés avant/après la commande (un échec de `before`
> abandonne l'opération ; `after` s'exécute même si la commande échoue). Spec :
> `docs/specs/liora-toolchain.md`. Exit codes : `1` résolution impossible, `130` interruption
> (Ctrl+C), sinon le code de la commande.
>
> **Porte de santé (pre-flight)** — `dev`, `build` et `start` exécutent d'abord l'`audit` de
> conformité des modules : un module en erreur annule la commande (warnings non bloquants,
> sans module le contrôle est ignoré). Les contrôles `debug`/`test` restent des commandes
> dédiées (`liora debug`, `liora test`) et ne sont plus lancés par la porte.
>
> **`artifact` est la chaîne de développement du module, pas un passthrough** — `liora artifact
> <action>` réimplémente la chaîne dans le binaire `liora` (`internal/artifactdev` pour le build, le
> dev-server et le pack, `internal/artifactbind` pour la liaison au socle, esbuild appelé par son
> **API Go**) : le module n'a plus à déclarer de dépendance ni à embarquer de `node_modules`. Le
> module visé est celui qui porte le répertoire courant, sinon celui nommé en argument, sinon celui
> sélectionné depuis la racine du projet — `liora artifact build modules/blog-manager` et
> `cd modules/blog-manager && liora artifact build` visent donc la même cible. L'extension
> `.LiorArtifactPackage` (ZIP renommé) est produite par `artifact pack` — et par `liora pack` pour la
> chaîne de publication (signature, store).
>
> **`bind:socle` / `unbind:socle` prennent le dossier du socle en premier argument** — `liora`
> l'absolutise contre le répertoire d'appel, résout le module (répertoire courant, argument ou
> sélection depuis la racine), puis pose ou retire la liaison.

## Variables d'environnement

| Variable | Description |
|----------|-------------|
| `LIORIAN_AUTH_API` | URL de base de l'API liorian-auth (authentications) |
| `LIORIAN_CLI_AUTH_CODE` | Code d'autorisation OAuth2 pour `liora auth` en mode non interactif (CI) |
| `LIORIAN_CLI_DEBUG` | Active les logs détaillés (`--verbose` équivalent) |
| `LIORIAN_CLI_ENV_AUTO` | Équivalent de `--auto-env` : `init` accepte toutes les valeurs suggérées du `.env` sans question |
| `LIORIAN_CLI_LANG` | Force la langue d'interface (`fr-FR`, `en-US`) |
| `LIORIAN_MODULE_MOCKUP` | Répertoire du module de référence pour `create module` (remplace le mockup embarqué du type choisi ; son `id` d'exemple sert de base au renommage) |
| `LIORIAN_PAGE_MOCKUP` | Gabarit `page.tsx` pour `create module` (repli sur le mockup embarqué) |
| `LIORIAN_VIEW_MOCKUP` | Fichier de vue de référence pour `create view` (repli sur le mockup embarqué) |
| `LIORIAN_STORE_API` | URL de base de l'API du catalogue pour `liora marketplace` (défaut : `api.baseUrl` de `liorian-store`) |
| `LIORIAN_CLI_TEMPLATE_REPO` | Source du template `init` (repo GitHub, URL ZIP directe ou répertoire local) |
| `LIORIAN_CLI_UPDATE_URL` | Endpoint du check auto-update (défaut : releases GitHub) |
| `LIORIAN_CLI_SKIP_UPDATE` | Désactive le check auto-update (réseau coupé) |

## Sécurité

- Credentials stockés dans le keychain système, jamais en clair sur disque
- Repli : fichier chiffré AES-256-GCM (`~/.liorian-cli/credentials.enc`)
- Clés de signature Ed25519 dans le keychain (service `liorian-cli-signing`),
  avec fichier chiffré en repli (`~/.liorian-cli/signing.enc`)
- Archives `.LiorArtifactPackage` : ZIP contenant `library/modules/<module>/` + `public/assets/<module>/` + `src/app/<module>/` + `manifest.json` canonique à la racine — aucun token ni credential ; signature Ed25519 obligatoire, vérification fail-closed

## Tests

```bash
go test ./...
go vet ./...
```

## Build & distribution

```bash
# Binaire local (la version/branch/commit/date viennent de `app.config.json`)
go build -o liora .

# Ou avec override ldflags
go build -ldflags "-X main.version=$(git describe --tags) -X main.branch=$(git branch --show-current) -X main.commit=$(git rev-parse --short HEAD)" -o liora .

# Release multi-plateforme
goreleaser release --clean
```