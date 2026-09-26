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
| `liora create module [nom] [--domain d] [--type TYPE] [--category CAT] [--mockup dir] [--page-mockup file]` | Créer un module dans `library/modules/` depuis le mockup hello-world canonique (miroir du socle : `compatibility`, `oauth`, `capabilities`, permissions `Role:Verbe`), surchargeable via `--mockup` / `--page-mockup` (`LIORIAN_MODULE_MOCKUP` / `LIORIAN_PAGE_MOCKUP`) ; `--type` (enum `ModuleType`, défaut `WEB_APP_LOCAL`) et `--category` (défaut `SYSTEM`) sont écrits dans le manifeste et la déclaration |
| `liora create view <module> [nom] [--name id] [--label titre] [--description texte] [--mockup file]` | Créer une vue de présentation (`presentation/views/<id>.view.tsx`) dans un module existant depuis le mockup embarqué (composant `<Id>View`, titre et description renommés) |
| `liora connect` | Authentification via liorian-connect (email + mot de passe, MFA TOTP / backup codes) |
| `liora auth` | Authentification OAuth2 (code d'autorisation + PKCE) via le navigateur — endpoints issus de `app.config.json` (`oauth` de `liorian-auth`) ; mode CI via `LIORIAN_CLI_AUTH_CODE` |
| `liora disconnect` | Invalider le token côté serveur et supprimer les credentials |
| `liora pack [module[@version]] [--version V] [--out F]` | Construire l'archive `.liozip` dans `.lorian/build/` (manifeste canonique à la racine, quotas 50 Mo/5 000 fichiers/ratio 100:1, empreintes SHA-256 affichées) |
| `liora sign keygen` | Générer une paire de clés Ed25519 pour la signature |
| `liora sign [module\|archive.liozip] [--key F]` | Signer la charge utile canonique de publication (Ed25519, obligatoire pour publier — ADR-010) |
| `liora sign verify [module\|archive.liozip]` | Vérifier la signature d'un module (repli legacy accepté, signalé) |
| `liora publish [module] [--version V] [--file F] [--allow-unsigned]` | Auditer, packer, signer (obligatoire) et publier un module sur le store (`manifestChecksum` + `signatureKeyId` déclarés) |
| `liora link` / `unlink` | Associer un module local à un module distant du store (token) |
| `liora marketplace search\|install` | Rechercher (`search [query]`) et installer (`install <module>`) des modules depuis le catalogue public (`LIORIAN_STORE_API`) |
| `liora audit [module]` | Auditer la conformité (Clean Architecture, manifest, dépendances) |
| `liora repair [module] [--dry-run] [--warnings] [--no-install] [--no-interaction] [--output table\|json]` | Réparer automatiquement les anomalies bloquantes d'un (ou tous les) module(s) : champs de manifeste, nom du dossier, dépendances npm manquantes, JSON malformé ; les points non réparables deviennent des instructions pas à pas |
| `liora debug [module]` | Valider le module et lancer un build de diagnostic |
| `liora test [module]` | Exécuter les tests via le gestionnaire choisi à l'installation (script `test`, vitest/jest, `bun test`) ; package de test persisté dans `lorian.config.json` ; exit `13` en cas d'échec |
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

`lorian.config.json` (optionnel, à la racine du projet) :

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

## Variables d'environnement

| Variable | Description |
|----------|-------------|
| `LIORIAN_AUTH_API` | URL de base de l'API liorian-auth (authentications) |
| `LIORIAN_CLI_AUTH_CODE` | Code d'autorisation OAuth2 pour `liora auth` en mode non interactif (CI) |
| `LIORIAN_CLI_DEBUG` | Active les logs détaillés (`--verbose` équivalent) |
| `LIORIAN_CLI_ENV_AUTO` | Équivalent de `--auto-env` : `init` accepte toutes les valeurs suggérées du `.env` sans question |
| `LIORIAN_CLI_LANG` | Force la langue d'interface (`fr-FR`, `en-US`) |
| `LIORIAN_MODULE_MOCKUP` | Répertoire du module de référence pour `create module` (repli sur le mockup embarqué) |
| `LIORIAN_PAGE_MOCKUP` | Gabarit `page.tsx` pour `create module` (repli sur le mockup embarqué) |
| `LIORIAN_VIEW_MOCKUP` | Fichier de vue de référence pour `create view` (repli sur le mockup embarqué) |
| `LIORIAN_STORE_API` | URL de base de l'API du catalogue pour `liora marketplace` (défaut : `api.baseUrl` de `liorian-store`) |
| `LIORIAN_CLI_TEMPLATE_REPO` | Source du template `init` (repo GitHub, URL ZIP directe ou répertoire local) |
| `LIORIAN_CLI_UPDATE_URL` | Endpoint du check auto-update (défaut : releases GitHub) |
| `LIORIAN_CLI_SKIP_UPDATE` | Désactive le check auto-update (réseau coupé) |

## Sécurité

- Credentials stockés dans le keychain système, jamais en clair sur disque
- Repli : fichier chiffré AES-256-GCM (`~/.lorian-cli/credentials.enc`)
- Clés de signature Ed25519 dans le keychain (service `lorian-cli-signing`),
  avec fichier chiffré en repli (`~/.lorian-cli/signing.enc`)
- Archives `.liozip` : ZIP contenant `library/modules/<module>/` + `public/assets/<module>/` + `src/app/<module>/` + `manifest.json` canonique à la racine — aucun token ni credential ; signature Ed25519 obligatoire, vérification fail-closed

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