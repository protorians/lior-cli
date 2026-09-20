# Lior CLI

Outil de développement en ligne de commande pour créer, maintenir et publier
des modules dans l'écosystème Liorian (spécification : `docs/specs/liorian.md`).

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
go build -o liorian .
./liorian --help
```

## Installation (distribution)

### Homebrew (macOS / Linux)

```bash
brew tap protorians/lior-cli https://github.com/protorians/lior-cli.git
brew install protorians/lior-cli/liorian
```

> Homebrew exige une référence `user/repo/formula` (3 segments) : la forme
> `brew install protorians/lior-cli` (2 segments) n'est pas valide dans Homebrew.

### Go / npm / binaire

Voir la section « Installation » de la spécification (`docs/specs/liorian.md`,
§10.2) pour les canaux `go install`, `npm`/`npx`, script `curl` et Windows.

## Commandes

| Commande | Description |
|----------|-------------|
| `liorian init [--channel alpha|beta|rc|stable]` | Télécharger la release ZIP de `protorians/liorian-socle` (canal stable par défaut) + installer les dépendances (détection bun/pnpm/yarn/npm) |
| `liorian create module [nom] [--domain d] [--type INTERNAL\|EXTERNAL] [--category CAT] [--mockup dir] [--page-mockup file]` | Créer un module dans `library/modules/` depuis le mockup hello-world (renommé avec le nom du module), surchargeable via `--mockup` / `--page-mockup` (`LIORIAN_MODULE_MOCKUP` / `LIORIAN_PAGE_MOCKUP`) ; `--type` (défaut `EXTERNAL`) et `--category` (défaut `SYSTEM`) sont écrits dans le manifeste et la déclaration |
| `liorian connect` | Authentification via liorian-connect (email + mot de passe, MFA TOTP / backup codes) |
| `liorian auth` | Authentification OAuth2 (code d'autorisation + PKCE) via le navigateur — endpoints issus de `app.config.json` (`oauth` de `liorian-auth`) ; mode CI via `LIORIAN_CLI_AUTH_CODE` |
| `liorian disconnect` | Invalider le token côté serveur et supprimer les credentials |
| `liorian pack [module]` | Construire l'archive `.SenMod` dans `.lorian/build/` |
| `liorian sign keygen` | Générer une paire de clés Ed25519 pour la signature |
| `liorian sign [module]` | Signer l'archive `.SenMod` d'un module |
| `liorian sign verify [module]` | Vérifier la signature d'un module |
| `liorian publish [module]` | Auditer, packer et publier un module sur le store |
| `liorian link` / `unlink` | Associer un module local à un module distant du store (token) |
| `liorian audit [module]` | Auditer la conformité (Clean Architecture, manifest, dépendances) |
| `liorian debug [module]` | Valider le module et lancer un build de diagnostic |
| `liorian test [module]` | Exécuter les tests via le gestionnaire choisi à l'installation (script `test`, vitest/jest, `bun test`) ; package de test persisté dans `lorian.config.json` ; exit `13` en cas d'échec |
| `liorian dev [-- args]` | Démarrer le serveur de développement de l'application (script `dev`) via le gestionnaire choisi à l'installation (Ctrl+C pour arrêter) ; exécute d'abord `debug` + `test` sur les modules |
| `liorian build [-- args]` | Construire l'application (script `build`) via le gestionnaire choisi à l'installation ; exécute d'abord `debug` + `test` + `audit` sur les modules ; exit = code de la commande |
| `liorian start [-- args]` | Démarrer l'application en production (script `start`) via le gestionnaire choisi à l'installation ; exécute d'abord `debug` + `test` + `audit` sur les modules |
| `liorian check [-- args]` | Exécuter l'analyse statique (script `lint` par défaut, remappable via `toolchain.commands.check`) ; exit = code de la commande |
| `liorian -v` / `--version` | Afficher la version |
| `liorian help` | Aide contextuelle |

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

> Le bloc `test` est écrit automatiquement par `liorian test` : `packageManager` reprend le
> gestionnaire choisi à l'installation, `runner` le package de test par défaut et
> `modules.<domaine>.runner` une surcharge par module (`vitest`, `jest`, `mocha`, `ava`, un package
> personnalisé, `builtin` ou `script`).
>
> Le bloc `toolchain` pilote `liorian dev/build/start/check` : `commands` remappe la commande vers un
> script `package.json` (défauts : `dev`→`dev`, `build`→`build`, `start`→`start`, `check`→`lint`),
> `before`/`after` listent les scripts exécutés avant/après la commande (un échec de `before`
> abandonne l'opération ; `after` s'exécute même si la commande échoue). Spec :
> `docs/specs/liorian-toolchain.md`. Exit codes : `1` résolution impossible, `130` interruption
> (Ctrl+C), sinon le code de la commande.
>
> **Porte de santé (pre-flight)** — `dev` vérifie d'abord les modules (`debug` + `test`),
> `build`/`start` ajoutent `audit` (`debug` + `test` + `audit`) : un module en erreur annule
> la commande (warnings non bloquants, sans module le contrôle est ignoré).

## Variables d'environnement

| Variable | Description |
|----------|-------------|
| `LIORIAN_AUTH_API` | URL de base de l'API liorian-auth (authentications) |
| `LIORIAN_CLI_AUTH_CODE` | Code d'autorisation OAuth2 pour `liorian auth` en mode non interactif (CI) |
| `LIORIAN_CLI_DEBUG` | Active les logs détaillés (`--verbose` équivalent) |
| `LIORIAN_CLI_LANG` | Force la langue d'interface (`fr-FR`, `en-US`) |
| `LIORIAN_MODULE_MOCKUP` | Répertoire du module de référence pour `create module` (repli sur le mockup embarqué) |
| `LIORIAN_PAGE_MOCKUP` | Gabarit `page.tsx` pour `create module` (repli sur le mockup embarqué) |
| `LIORIAN_CLI_TEMPLATE_REPO` | Source du template `init` (repo GitHub, URL ZIP directe ou répertoire local) |
| `LIORIAN_CLI_UPDATE_URL` | Endpoint du check auto-update (défaut : releases GitHub) |
| `LIORIAN_CLI_SKIP_UPDATE` | Désactive le check auto-update (réseau coupé) |

## Sécurité

- Credentials stockés dans le keychain système, jamais en clair sur disque
- Repli : fichier chiffré AES-256-GCM (`~/.lorian-cli/credentials.enc`)
- Clés de signature Ed25519 dans le keychain (service `lorian-cli-signing`),
  avec fichier chiffré en repli (`~/.lorian-cli/signing.enc`)
- Archives `.SenMod` : ZIP contenant uniquement `library/modules/<module>/` +
  `public/assets/<module>/` + `src/app/<module>/` — aucun token ni credential

## Tests

```bash
go test ./...
go vet ./...
```

## Build & distribution

```bash
# Binaire local
go build -ldflags "-X main.version=$(git describe --tags) -X main.commit=$(git rev-parse --short HEAD)" -o liorian .

# Release multi-plateforme
goreleaser release --clean
```