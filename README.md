# Sentient CLI

Outil de développement en ligne de commande pour créer, maintenir et publier
des modules dans l'écosystème Sentient (spécification : `docs/specs/sentient.md`).

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
go build -o sentients .
./sentients --help
```

## Commandes

| Commande | Description |
|----------|-------------|
| `sentients init [--channel alpha|beta|rc|stable]` | Télécharger la release ZIP de `protorians/sentients-socle` (canal stable par défaut) + installer les dépendances (détection bun/pnpm/yarn/npm) |
| `sentients create module [nom] [--mockup dir] [--page-mockup file]` | Créer un module dans `external_modules/` depuis le mockup hello-world (renommé avec le nom du module), surchargeable via `--mockup` / `--page-mockup` (`SENTIENT_MODULE_MOCKUP` / `SENTIENT_PAGE_MOCKUP`) |
| `sentients connect` | Authentification via sentient-connect (email + mot de passe, MFA TOTP / backup codes) |
| `sentients disconnect` | Invalider le token côté serveur et supprimer les credentials |
| `sentients pack [module]` | Construire l'archive `.SenMod` dans `.sentients/build/` |
| `sentients sign keygen` | Générer une paire de clés Ed25519 pour la signature |
| `sentients sign [module]` | Signer l'archive `.SenMod` d'un module |
| `sentients sign verify [module]` | Vérifier la signature d'un module |
| `sentients publish [module]` | Auditer, packer et publier un module sur le store |
| `sentients link` / `unlink` | Associer un module local à un module distant du store (token) |
| `sentients audit [module]` | Auditer la conformité (Clean Architecture, manifest, dépendances) |
| `sentients debug [module]` | Valider le module et lancer un build de diagnostic |
| `sentients -v` / `--version` | Afficher la version |
| `sentients help` | Aide contextuelle |

## Configuration

`sentients.config.json` (optionnel, à la racine du projet) :

```json
{
  "project": {
    "name": "mon-projet",
    "packageManager": "bun"
  },
  "publish": {
    "defaultRegistry": "https://store.sentient.dev",
    "autoAudit": true
  },
  "debug": {
    "verbose": false,
    "logLevel": "info"
  }
}
```

## Variables d'environnement

| Variable | Description |
|----------|-------------|
| `SENTIENT_AUTH_API` | URL de base de l'API sentient-auth (authentications) |
| `SENTIENT_CLI_DEBUG` | Active les logs détaillés (`--verbose` équivalent) |
| `SENTIENT_CLI_LANG` | Force la langue d'interface (`fr-FR`, `en-US`) |
| `SENTIENT_MODULE_MOCKUP` | Répertoire du module de référence pour `create module` (repli sur le mockup embarqué) |
| `SENTIENT_PAGE_MOCKUP` | Gabarit `page.tsx` pour `create module` (repli sur le mockup embarqué) |
| `SENTIENT_CLI_TEMPLATE_REPO` | Source du template `init` (repo GitHub, URL ZIP directe ou répertoire local) |

## Sécurité

- Credentials stockés dans le keychain système, jamais en clair sur disque
- Repli : fichier chiffré AES-256-GCM (`~/.sentient-cli/credentials.enc`)
- Clés de signature Ed25519 dans le keychain (service `sentient-cli-signing`),
  avec fichier chiffré en repli (`~/.sentient-cli/signing.enc`)
- Archives `.SenMod` : ZIP contenant uniquement `external_modules/<module>/` +
  `public/assets/<module>/` + `src/app/<module>/` — aucun token ni credential

## Tests

```bash
go test ./...
go vet ./...
```

## Build & distribution

```bash
# Binaire local
go build -ldflags "-X main.version=$(git describe --tags) -X main.commit=$(git rev-parse --short HEAD)" -o sentients .

# Release multi-plateforme
goreleaser release --clean
```