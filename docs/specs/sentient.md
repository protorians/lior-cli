# Sentients CLI (`sentients`)

> **Statut : IMPLÉMENTÉ (binaire `sentients`, spec alignée sur le code)**
>
> Ce document est la **spécification SpecKit de la CLI `sentients`**, outil en ligne de commande
> permettant aux développeurs d'initialiser, créer, construire, auditer, déboguer et publier des
> modules Sentient via un compte développeur `sentient-connect`.
>
> - **Stack technique** : Go (1.26, Cobra) + Bubbletea (TUI lipgloss/charmbracelet)
> - **Distribution** : binaire unique multi-plateforme (Linux, macOS, Windows)
> - **État du code** : implémenté dans `protorians/sentient-cli` (branche `alpha`) ; dernière release documentée 0.7.0 ;
>   l'écart constaté entre la spec et le code est documenté dans `docs/rapport-implementation.md`
>
> **Documents de référence (workspace `sentient-workspace/docs`)** : la présente spec s'aligne sur
> les contrats du workspace, en particulier le manifest de module
> (`docs/modules/module-manifest.md`, schéma JSON `@sentients/sdk/domain/schemas/module-manifest.schema.json`)
> et la distribution des responsabilités Connect / Store / Core
> (`docs/specs/applications/module-distribution.md`, `docs/modules/store.md`, `docs/specs/applications/sentient-connect.md`).

---

## Métadonnées SpecKit

| Propriété | Valeur |
|-----------|--------|
| Identifiant | `sentients` |
| Nom | Sentient CLI |
| Rôle | Outil CLI pour le cycle de vie complet des modules Sentient |
| Type de spécification | Application Spec |
| Version de spécification | `0.1.0` (candidate) |
| Statut de la version | `active` (spec) — implémentée (rel. 0.7.0) |
| Langue | Document en français ; interface bilingue fr-FR / en-US (i18n §11.2) |
| Emplacement cible (SpecKit) | `sentient.md` |

---

## 1. Vision Produit

### 1.1 Objectif

Sentient CLI est l'outil de développement unique pour tout développeur souhaitant créer, maintenir
et publier des modules dans l'écosystème Sentient. Elle couvre le cycle de vie complet :

```
init → create → develop → debug → audit → pack → sign → link → publish
                ↑                                               │
                └───────────────────────────────────────────────┘
```

### 1.2 Public cible

| Acteur | Usage |
|--------|-------|
| Développeur Sentient | Initialiser un projet, créer/modifier des modules, publier sur le store |
| Équipe interne | Audit automatique, validation des conventions, debug |

### 1.3 Valeur ajoutée

- **Zéro configuration manuelle** : détection automatique des outils disponibles sur la machine
- **Sécurité native** : credentials chiffrés, MFA supportée, token rotation
- **Validation continue** : audit des règles Clean Architecture + conformité manifest
- **Intégration Sentient Connect** : publication one-shot vers le store

---

## 2. Portée

### Dans le périmètre (In Scope)

- `sentients init` — Initialisation d'un projet Sentient (téléchargement de la release template + deps)
- `sentients create module` — Création de module dans `external_modules/`
- `sentients connect` — Authentification développeur (credentials + MFA)
- `sentients auth` — Authentification OAuth2 (code d'autorisation + PKCE, navigation navigateur)
- `sentients disconnect` — Suppression des credentials
- `sentients pack` — Build + compression d'un module (`.SenMod`)
- `sentients sign` — Signature numérique Ed25519 des archives `.SenMod` (keygen / sign / verify)
- `sentients publish` — Publication dans le store via Sentient Connect
- `sentients link` — Liaison module local ↔ module en ligne
- `sentients unlink` — Dé liaison module local ↔ module en ligne
- `sentients debug <module>` — Debug d'un ou tous les modules
- `sentients audit <module>` — Audit de conformité d'un ou tous les modules
- `sentients help` — Affichage de l'aide
- `sentients -v | --version` — Affichage de la version

### Hors périmètre (Out of Scope)

- Gestion du contenu des modules (pages, composants, API)
- Monitoring temps réel des modules en production
- Gestion des organisations / équipes
- CI/CD intégré (workflow GitHub Actions séparé)

### Périmètre futur (Future Scope)

- `sentients test <module>` — Exécution des tests d'un module
- `sentients watch` — Mode développement hot-reload
- `sentients deploy` — Déploiement direct vers un environnement
- `sentients marketplace` — Recherche/installation de modules tiers

---

## 3. Exigences

### Exigences fonctionnelles

| ID | Description |
|----|-------------|
| FR-001 | La CLI détecte automatiquement les gestionnaires de paquets disponibles (bun, pnpm, yarn, npm) et propose le choix à l'utilisateur |
| FR-002 | `sentients init` télécharge la release (ZIP) du template `protorians/sentients-socle` dans le répertoire courant, selon un canal (`stable` par défaut, `alpha`, `beta`, `rc`) |
| FR-003 | `sentients init` installe les dépendances avec le gestionnaire choisi |
| FR-004 | `sentients create module` crée un module dans `external_modules/<domain>/` (domaine reverse-DNS + identifiant kebab-case) à partir d'un mockup de référence embarqué (Clean Architecture, structure standardisée) |
| FR-005 | `sentients create module` génère un token UUID unique dans `manifest.json` et un manifeste conforme au schéma du workspace (`module-manifest.schema.json`, `schemaVersion: 1`) |
| FR-006 | `sentients connect` authentifie le développeur via `sentient-connect` (email + mot de passe) |
| FR-007 | `sentients connect` supporte le MFA (TOTP, backup codes) |
| FR-008 | `sentients connect` stocke les credentials de manière sécurisée (keychain/credential store, fallback vault chiffré) |
| FR-009 | `sentients disconnect` supprime toutes les credentials stockées |
| FR-010 | `sentients pack` compresse `external_modules/<module>/` + `public/assets/<module>/` + `src/app/<module.uri>/` en `.SenMod` |
| FR-011 | `sentients pack` déplace l'archive vers `.sentients/build/` |
| FR-012 | `sentients publish` construit, audite puis publie via l'API developer-store (produit → version → artefact) |
| FR-013 | `sentients publish` demande les métadonnées du module si non définies |
| FR-014 | `sentients link` lie un module local à un module distant (token produit, mode CI `link <module> <token>`) et persiste l'état dans `.sentients/links.json` |
| FR-015 | `sentients unlink` délie un module local de `sentient-connect` (option `--sync-remote` pour synchroniser les métadonnées locales) |
| FR-016 | `sentients debug` lance le debug d'un module ou de tous les modules |
| FR-017 | `sentients audit` vérifie la conformité Clean Architecture, `manifest.json` et `index.tsx` |
| FR-018 | `sentients audit` vérifie que les `requirements` et `dependencies` existent |
| FR-019 | `sentients help` affiche l'aide contextuelle des commandes |
| FR-020 | `sentients -v` / `sentients --version` affiche la version actuelle |
| FR-021 | `sentients sign keygen` génère une paire de clés Ed25519 et la stocke dans le keychain système |
| FR-022 | `sentients sign <module>` signe l'archive `.SenMod` du module et produit un fichier `.sig` |
| FR-023 | `sentients sign verify <module>` vérifie la validité de la signature `.sig` d'un module |
| FR-024 | `sentients sign` affiche le fingerprint SHA-256 de la clé publique du développeur |
| FR-025 | La langue de l'interface est résolue dans l'ordre : `--lang` → `SENTIENT_CLI_LANG` → `cli.lang` de `sentients.config.json` → locale OS (LC_ALL/LC_MESSAGES/LANG), avec repli sur `en-US` |
| FR-026 | `sentients auth` authentifie le développeur via le flux OAuth2 **code d'autorisation + PKCE** (navigateur + serveur local en boucle), complément du `sentients connect` (email/mot de passe) |
| FR-027 | `sentients auth` stocke la session OAuth (`access_token`, `refresh_token`, expiration) dans le keychain (repli vault chiffré) et la partage avec les commandes authentifiées |

### Exigences non-fonctionnelles

| ID | Description |
|----|-------------|
| NFR-001 | Binaire unique, sans dépendance externe (static linking) |
| NFR-002 | Temps de démarrage < 100ms |
| NFR-003 | Compatible Linux (amd64, arm64), macOS (amd64, arm64), Windows (amd64) |
| NFR-004 | Sortie terminal compatible UTF-8 + 256 couleurs minimum |
| NFR-005 | Logs activables via `--verbose` ou variable d'environnement `SENTIENT_CLI_DEBUG` |
| NFR-006 | Mises à jour auto-detectées (notification, pas de mise à jour forcée) |
| NFR-007 | Interface bilingue `en-US` (défaut) / `fr-FR` via catalogues i18n embarqués dans le binaire, avec repli sur `en-US` |

### Exigences de sécurité

| ID | Description |
|----|-------------|
| SEC-001 | Credentials stockés dans le keychain système (macOS Keychain, Linux secret-service, Windows Credential Manager) |
| SEC-002 | Jamais de credentials en clair sur disque (pas de `.env`, pas de fichier texte) |
| SEC-003 | Tokens d'accès avec durée de vie limitée, rotation automatique |
| SEC-004 | Chiffrement des données sensibles au repos (AES-256-GCM pour les caches) |
| SEC-005 | Validation stricte des inputs (UUID, noms de module, URLs) |
| SEC-006 | Mode MFA obligatoire si activé sur le compte développeur |
| SEC-007 | Les archives `.SenMod` ne contiennent jamais de credentials ou tokens |
| SEC-008 | Les clés de signature Ed25519 sont stockées dans le keychain OS, jamais en clair sur disque |
| SEC-009 | La signature numérique garantit l'intégrité et l'authenticité des archives `.SenMod` avant publication |

### Exigences techniques

| ID | Description |
|----|-------------|
| TECH-001 | Go 1.22+ comme langage de développement (go.mod : 1.26) |
| TECH-002 | Bubbletea comme framework TUI pour les interactions utilisateur |
| TECH-003 | Lipgloss pour le styling terminal |
| TECH-004 | Bubbles pour les composants TUI réutilisables — priorité stricte aux composants natifs bubbles avant tout composant custom (voir §9.1) |
| TECH-005 | GoReleaser pour la compilation multi-plateforme et le packaging |
| TECH-006 | Architecture en couches : commands → services → infrastructure |
| TECH-007 | Configuration via fichier `sentients.config.json` optionnel dans le projet |
| TECH-008 | Communication avec `sentient-connect` via REST API HTTPS |
| TECH-009 | Registre d'applications `app.config.json` embarqué dans le binaire (`baseUrl`/`timeout` par application API), surchargeable par un `app.config.json` local et `SENTIENT_AUTH_API` |

---

## 4. Architecture

### 4.1 Architecture en couches

```
sentient-cli/
├── main.go                        # Point d'entrée (variables version/commit/date + //go:embed app.config.json)
├── cmd/                           # Commandes CLI (couche présentation, Cobra)
│   ├── root.go                    # Commande racine (flags --verbose, --no-color, --lang, update check)
│   ├── init.go                    # sentients init (--channel alpha|beta|rc|stable)
│   ├── create.go                  # sentients create module
│   ├── connect.go                 # sentients connect
│   ├── auth.go                    # sentients auth (OAuth2 code + PKCE)
│   ├── disconnect.go              # sentients disconnect
│   ├── pack.go                    # sentients pack
│   ├── sign.go                    # sentients sign (keygen / sign / verify)
│   ├── publish.go                 # sentients publish
│   ├── link.go                    # sentients link + unlink (--sync-remote)
│   ├── debug.go                   # sentients debug
│   ├── audit.go                   # sentients audit (--output table|json)
│   ├── modules.go                 # Helpers de résolution projet/module (code 3)
│   └── localize.go                # Helpers i18n (MessageKey, résolution langue)
├── internal/
│   ├── config/                    # Configuration projet & CLI
│   │   ├── config.go              # Lecture/écriture sentients.config.json
│   │   └── paths.go               # Résolution des chemins projet
│   ├── appconfig/                 # Registre d'applications embarqué (app.config.json, TECH-009)
│   │   └── appconfig.go           # BaseURL/timeout par API, surcharge locale/env
│   ├── i18n/                      # Internationalisation (NFR-007)
│   │   ├── i18n.go                # Résolution langue, lookup de clés, fallback en-US
│   │   └── locales/               # Catalogues embarqués en-US.json, fr-FR.json
│   ├── auth/                      # Authentification & credentials
│   │   ├── credentials.go         # Keychain + fallback vault chiffré (SENTIENT_CLI_STORE)
│   │   ├── connector.go           # Client API sentient-connect
│   │   ├── mfa.go                 # Logique MFA (TOTP, backup codes)
│   │   ├── oauth.go               # Flux OAuth2 authorization_code + PKCE (navigateur, callback loopback)
│   │   └── session.go             # Session locale (token cache, refresh)
│   ├── module/                    # Logique module
│   │   ├── creator.go             # Création de module
│   │   ├── scaffold.go            # Scaffolding depuis le mockup embarqué (renommage arborescence)
│   │   ├── mockups/               # hello-world/ + page.tsx (mockups embarqués)
│   │   ├── manifest.go            # Manipulation manifest.json
│   │   ├── packer.go              # Compression .SenMod (limite 50 MB)
│   │   ├── linker.go              # Liaison local ↔ distant + état .sentients/links.json
│   │   ├── validator.go           # Validation module
│   │   └── module_test.go         # Tests unitaires
│   ├── signing/                   # Signature numérique Ed25519
│   │   ├── signer.go              # Génération clés, signature, vérification
│   │   └── keystore.go            # Stockage clés (keychain + fallback chiffré signing.enc)
│   ├── audit/                     # Audit de conformité
│   │   └── auditor.go             # Orchestrateur d'audit (règles manifest/bootstrap/deps)
│   ├── debug/                     # Debug de module
│   │   └── debugger.go            # Build/test du module (scripts ou tsc --noEmit)
│   ├── store/                     # Publication store
│   │   ├── builder.go             # Construction archive
│   │   └── publisher.go           # Publication via API developer-store (produit → version → artefact)
│   ├── tui/                       # Composants Bubbletea
│   │   ├── components.go          # SummaryCard, Wordmark, StepsList, LogsBlock (lipgloss)
│   │   ├── styles.go              # Palette brand sage/olive + thème dark/light
│   │   ├── prompts.go             # AskText, Confirm, Select (degradation non-interactive)
│   │   ├── spinner.go             # RunWithSpinner (indicateur de progression)
│   │   ├── progress.go            # RunWithProgress (barre de progression, téléchargements)
│   │   └── table.go               # Tableau arrondi custom (lipgloss)
│   └── pkg/                       # Utilitaires
│       ├── errors.go              # Erreurs catégorisées + codes de sortie §11.1
│       ├── fs.go                  # Opérations fichiers
│       ├── git.go                 # Exécution de commandes externes
│       ├── github.go              # FetchReleaseZip (téléchargement release init)
│       ├── http.go                # Client HTTP + enveloppe Raiton + APIError
│       ├── uuid.go                # Génération UUID
│       ├── crypto.go              # MachineSecret (PBKDF2), EncryptVault/DecryptVault (AES-256-GCM)
│       ├── open.go                # Ouverture du navigateur (open / rundll32 / xdg-open)
│       └── update.go              # Détection de mises à jour (NFR-006, cache 24 h)
├── e2e/                           # Tests E2E
│   ├── e2e_test.go                # Générateur testscript (TC-001 → TC-025 vs mock API)
│   └── testdata/                  # scripts/*.txtar + fixtures/ (bun, node, npm, tsc, mock API)
├── app.config.json                # Registre embarqué des applications (surchargeable localement)
├── go.mod
├── go.sum
├── .goreleaser.yaml               # Configuration GoReleaser
└── README.md
```

### 4.2 Dépendances Go

| Module | Usage | Version (go.mod) |
|--------|-------|-----------------|
| `github.com/spf13/cobra` | Parsing de commandes | `v1.10.2` |
| `github.com/charmbracelet/bubbletea` | Framework TUI | `v1.3.10` |
| `github.com/charmbracelet/lipgloss` | Styling terminal | `v1.1.0` |
| `github.com/charmbracelet/bubbles` | Composants TUI (spinner, textinput, list, progress) | `v1.0.0` |
| `github.com/zalando/go-keyring` | Accès keychain système | `v0.2.8` |
| `github.com/google/uuid` | Génération UUID v4 | `v1.6.0` |
| `github.com/muesli/termenv` | Détection terminal / profile couleur | `v0.16.0` |
| `rogpeppe/go-internal` | Tests E2E (testscript) | `v1.16.0` |
| `golang.org/x/term` | Détection terminal | `v0.46.0` |
| `net/http` | Client API (stdlib) | — |
| `crypto/aes`, `crypto/cipher` | Chiffrement (stdlib) | — |
| `crypto/ed25519` | Signature numérique Ed25519 (stdlib) | — |
| `archive/zip` | Compression .SenMod (stdlib) | — |

> Le parsing TOML (`BurntSushi/toml`) a été retiré : la configuration est uniquement JSON
> (`sentients.config.json`). `sentient.config.toml` ne sert plus que de marqueur de projet.

### 4.3 Pipeline d'exécution

```
Utilisateur
    │
    ▼
Commande (cmd/*.go)
    │ Validation args + flags
    ▼
Service (internal/module/*.go, internal/auth/*.go)
    │ Logique métier
    ▼
Infrastructure (internal/pkg/*.go, internal/config/*.go)
    │ Fichiers, HTTP, keychain, git
    ▼
Sortie TUI (internal/tui/*.go)
    │ Affichage interactif
    ▼
Utilisateur
```

---

## 5. Commandes — Spécification détaillée

---

### 5.1 `sentients init`

#### Purpose

Initialiser un nouveau projet Sentient en téléchargeant la release (ZIP) du template
`protorians/sentients-socle` et en installant les dépendances. La source est `--channel`
("stable" par défaut) ; `SENTIENT_CLI_TEMPLATE_REPO` peut la remplacer par une URL GitHub,
une URL ZIP directe ou un répertoire local (tests/miroirs).

#### Comportement

1. **Déterminer le nom du projet** : argument positionnel optionnel, sinon input Bubbletea
   (défaut : nom du dossier courant)
2. **Résoudre le canal de release** (`--channel alpha|beta|rc|stable`, défaut `stable`) :
   la release la plus récente du canal est téléchargée en ZIP via l'API GitHub
3. **Gérer la destination existante** : dossier non vide → proposer *Annuler* / *Fusionner* /
   *Vider* (cwd) / *Supprimer* ; jamais effacé sans approbation
4. **Détection automatique** des gestionnaires de paquets disponibles sur la machine :
   - `bun` → disponible ? (recommandé, premier du fil)
   - `pnpm` → disponible ?
   - `yarn` → disponible ?
   - `npm` → disponible ?
5. **Proposer le choix** via un sélecteur Bubbletea (liste filtrée aux disponibles)
6. **Télécharger la release** avec barre de progression `RunWithProgress` (extraction dans la
   destination ; en cas de fusion, téléchargement dans un dossier temporaire puis copie)
7. **Installer les dépendances** avec le gestionnaire sélectionné (échec → warning non bloquant)
8. **Écrire `sentients.config.json`** (racine `project.name` + `project.packageManager`)
9. **Afficher le résumé** : projet initialisé, gestionnaire utilisé, prochaines étapes

#### Contraintes

- Si aucun gestionnaire n'est détecté → erreur explicite avec instructions d'installation
- `--channel` doit être l'un des canaux valides (erreur listant les choix sinon)
- Pas de clone git : téléchargement de l'archive de release (rapide, sans historique git)
- En mode non-interactif, un dossier existant est vidé uniquement si `SENTIENT_CLI_YES` est défini, sinon refus explicite

#### Sortie TUI

```
Destination : /chemin/vers/mon-projet
? Nom du projet : mon-projet
? Canal de release : stable
? Gestionnaire de paquets : bun (recommandé)
  ⠋ Téléchargement de la release sentients-socle...
  [================--------------------] 45%
  ⠋ Installation des dépendances...

  ✓ Projet initialisé avec succès
    Gestionnaire : bun

  Prochaines étapes :
    cd mon-projet
    sentients connect
    sentients create module
```

---

### 5.2 `sentients create module`

#### Purpose

Créer un nouveau module dans `external_modules/<domain>/` à partir d'un **mockup de référence
embarqué** dans le binaire (Clean Architecture, structure standardisée) — FR-004. Aucun checkout
externe requis. Le manifeste produit respecte le contrat canonique du workspace
(`schemaVersion: 1`, `docs/modules/module-manifest.md`).

#### Comportement

1. **Vérifier le contexte** : être à la racine d'un projet Sentient (`sentients.config.json`,
   `sentient.config.toml` ou présence de `external_modules/`)
2. **Demander l'identité** du module (chaque prompt a un flag équivalent) :
   - **domaine** reverse-DNS (`--domain`, ex. `com.organization.domain`) — validation `ValidateDomain`,
     il nomme le dossier `external_modules/<domain>/` et le champ `manifest.domain`
   - **identifiant** kebab-case (`--id`, ou argument positionnel `create module <name>`) — 3-64,
     validation `ValidateName`, il nomme les composants et le champ `manifest.id`
   - **nom applicatif** affiché (`--name`, défaut : Title Case de l'identifiant)
   - **version** SemVer optionnelle (`--version`, défaut `0.0.0`)
   - **icône** lucide en PascalCase (`--icon`, défaut `PuzzleIcon`)
   - **url** de page (`--url`, défaut : identifiant) → `manifest.uri = /<url>` et
     `src/app/<url>/page.tsx`
   - **description** (`--description`)
   - **type** de distribution (`--type`, `INTERNAL` ou `EXTERNAL`, défaut `EXTERNAL`)
   - **catégorie** de store (`--category`, enum `ModuleCategory`, défaut `SYSTEM`)
3. **Résoudre la source du mockup module** (ordre de priorité) :
   - `--mockup` (champ `Creator.MockupDir`)
   - `SENTIENT_MODULE_MOCKUP` (répertoire de module de référence)
   - mockup **embarqué** `internal/module/mockups/hello-world/`
   (une source custom doit ressembler à un module scaffoldable : `manifest.json` + `index.tsx`)
4. **Scaffolder le module** (copie + renommage, `internal/module/scaffold.go`) :
   - Les fichiers et identifiants du mockup sont renommés selon les 6 variantes de
     l'**identifiant** (`Hello World` → affichable, `HelloWorld` → PascalCase,
     `helloWorld` → camelCase, `hello-world` → kebab-case, `HELLO_WORLD` → UPPER_SNAKE,
     `helloworld` → minuscules)
   - Le contenu des fichiers textes est réécrit en conséquence (renommage des fichiers inclus)
5. **Vérifier les requirements** : chaque module du champ `requirements` du `manifest.json` doit
   exister localement — dans `external_modules/` **ou** dans `src/modules/` (modules internes
   de la plate-forme). Les modules cœur de la plate-forme `organization` et `identity` sont
   toujours considérés satisfaits. En cas de module requis manquant, la création **échoue**,
   le répertoire scaffoldé ainsi que la page éventuelle sont **supprimés** (rollback) et une
   erreur catégorisée listant les modules manquants est renvoyée
   (`create.error.requirements_missing` + `create.error.requirements_missing.fix`)
6. **Installer les dépendances** : les `dependencies` et `devDependencies` du manifest sont
   résolues via le gestionnaire de paquets détecté (`bun` → `pnpm` → `yarn` → `npm`, voir
   `config.DetectPackageManager`) exécuté à la racine du projet. Si aucun gestionnaire n'est
   détecté, un avertissement est affiché (les dépendances ne sont pas installées). Un échec
   d'installation est non-bloquant (simple avertissement `create.warn.install`). L'installation
   peut être désactivée avec le drapeau `--skip-install` (ou l'environnement
   `SENTIENT_CLI_SKIP_INSTALL=1`).
7. **Patcher l'identité** (le mockup fournit les valeurs par défaut) :
   - `manifest.json` : injection d'un **token UUID v4 unique** si absent, puis réécriture de
     `id`, `domain`, `key` (UPPER_SNAKE de l'identifiant), `name`, `description`, `version`,
     `icon`, `type`, `category`, `uri`/`url`
   - `index.tsx` : `identifier` (= domaine), `key`, `version`, `name`, `description`, `icon`,
     `type`, `category`, `uri`/`url`
   - `package.json` : description mise à jour
   - `README.md` : généré (nom, description, structure)
8. **Scaffolder la page** : si la déclaration du module porte un `uri`/`url`, générer
   `src/app/<uri>/page.tsx` à partir du page mockup (ordre de priorité)
   `--page-mockup` (champ `Creator.PageMockup`) → `SENTIENT_PAGE_MOCKUP` → page **embarquée**
   `internal/module/mockups/page.tsx`
9. **Afficher le résumé** : module créé, token généré, page créée (si uri), prochaines étapes

#### Structure générée (mockup embarqué hello-world)

```
external_modules/<domain>/
├── manifest.json               # contrat module + token UUID v4 injecté
├── index.tsx                   # déclaration (identifier, widgets, service, routines, uri)
├── package.json                # dépendances du mockup
├── README.md
├── application/
│   └── service/                # <id>-api-service.ts (service de données)
├── domain/
│   ├── enums/                  # <id>-status.enum.ts (statuts)
│   └── <id>.interface.ts
├── infrastructure/
│   └── routines/               # <id>-analytics.routine.ts
└── presentation/
    ├── components/             # <id>-data-grid, -columns, -details-sheet, create-<id>-dialog
    ├── providers/              # <id>-header.provider.tsx (layout)
    ├── views/                  # <id>.view.tsx
    └── widgets/                # <id>.widget.tsx
```

> Les vues et conteneurs scaffoldés suivent la convention de disposition **`View` / `Activity`**
> du socle (`docs/frontend/view-activity.md`) : `View.Wrapper`, `View.Helmet`, `View.Frame`,
> `View.Status`, et `Activity.Container` / `Activity.Loader` (importés depuis `@sentients/sdk`).

`manifest.json` — le scaffold part du manifeste du mockup embarqué (miroir du module `hello-world`
du socle `sentient-socle`, conforme au contrat canonique du workspace) et réécrit `id`, `domain`,
`key`, `name`, `description`, `version`, `icon`, `uri` :

```json
{
  "$schema": "../../node_modules/@sentients/sdk/schemas/module.schema.json",
  "schemaVersion": 1,
  "id": "<id>",
  "domain": "<domain>",
  "key": "<ID_UPPER_SNAKE>",
  "name": "<Nom affiché>",
  "description": "<description>",
  "version": "<SemVer>",
  "icon": "<IconName>",
  "type": "<EXTERNAL|INTERNAL>",
  "entry": "index.tsx",
  "uri": "/<url>",
  "category": "<Catégorie>",
  "token": "<UUID v4 généré>",
  "publisher": { "id": "", "name": "" },
  "platforms": {
    "web": { "supported": true, "modes": ["web"] },
    "desktop": { "supported": true, "modes": ["local-webview"], "os": ["windows", "macos", "linux"] },
    "mobile": { "supported": true, "modes": ["local-webview"], "os": ["android"], "iosSupported": false }
  },
  "managerCompatibility": { "min": "0.17.1", "max": "0.17.x" },
  "apiCompatibility": { "min": "0.27.0", "max": "0.27.x" },
  "permissions": ["<id>.read", "<id>.write", "<id>.manage"],
  "apiScopes": ["<id>.read", "<id>.write"],
  "capabilities": {
    "needsNetwork": true,
    "supportsOffline": false,
    "requiresOrganization": true,
    "requiresAuthenticatedUser": true
  },
  "isEnabled": true,
  "isDefault": false,
  "requirements": { "organization": ">=1.0.0", "identity": ">=1.0.0" },
  "optionalRequirements": {},
  "dependencies": { "@sentients/sdk": "workspace:*", "react": "^19.0.0", "react-dom": "^19.0.0" },
  "devDependencies": { "typescript": "^6.0.3" },
  "widgets": ["analytics"],
  "routines": ["<camelId>AnalyticsRoutine"],
  "providers": ["layout"],
  "menu": { "items": [{ "label": "<Nom affiché>", "icon": "<IconName>", "url": "/<url>" }] }
}
```

> **Contrat canonique** : la référence des champs (identité, plateformes, compatibilité,
> permissions, capacités, activation, prérequis, dépendances, interface) et les conventions de
> nommage sont dans `docs/modules/module-manifest.md`. Le schéma JSON
> (`module-manifest.schema.json`, `schemaVersion: 1`) est publié avec le SDK
> (`@sentients/sdk`) et sert de référence à la validation CI. Les champs `requirements`,
> `optionalRequirements`, `dependencies` et `devDependencies` sont **strictement** gérés par le
> manifeste (jamais dupliqués dans `index.tsx`).

`index.tsx` (déclaration déclarative, pas de `render` asynchrone) :

```tsx
import {ModuleDeclarationInterface} from "@sentients/sdk/domain/entities/module.interface";

const <camelId>Module: ModuleDeclarationInterface = {
    identifier: '<domain>',
    key: '<ID_UPPER_SNAKE>',
    version: '<SemVer>',
    name: '<Nom affiché>',
    description: '<description>',
    icon: '<IconName>',
    uri: '/<url>',
    widgets: { analytics: <PascalId>Widget },
    service: { fetch: <PascalId>ApiService },
    routines: [<camelId>AnalyticsRoutine],
    providers: { layout: <PascalId>HeaderProvider },
    menu: { items: [{ label: '<Nom affiché>', icon: '<IconName>', url: '/<url>' }] },
    isEnabled: true,
    isDefault: false,
    type: '<EXTERNAL|INTERNAL>',
    category: '<Catégorie>',
};

export default <camelId>Module;
```

> **Séparation manifeste / déclaration** : `requirements`, `optionalRequirements`, `dependencies`
> et `devDependencies` sont **uniquement** déclarés dans `manifest.json` (voir
> `docs/modules/module-manifest.md` § 3) — ils ne figurent plus dans `index.tsx`. Le manifeste est
> la source de vérité ; la déclaration React en est la projection runtime.

`src/app/<url>/page.tsx` (scaffoldé quand la déclaration porte un `uri`/`url`) :

```tsx
import {<PascalId>View} from "@/external_modules/<domain>/presentation/views/<id>.view";

export default function <PascalId>Page() {
    return <<PascalId>View/>;
}
```

#### Contraintes

- Le token UUID est **unique** et généré à la création (injection dans le manifest scaffoldé)
- Le **domaine** (reverse-DNS) ne peut pas entrer en conflit avec un module existant
  (`external_modules/<domain>/`) ; l'identifiant doit être kebab-case (3-64)
- Le renommage est complet : fichiers **et** identifiants (imports, `identifier`, `key`, `uri`)
- Le manifeste est conforme au schéma canonique (24 champs requis, `schemaVersion: 1`) ;
  `entry` pointe vers `index.tsx` et `domain` correspond au dossier du module
- Les champs `requirements` / `optionalRequirements` / `dependencies` / `devDependencies` ne sont
  présents que dans `manifest.json` (jamais dans `index.tsx`)
- `--mockup` / `--page-mockup` (et `SENTIENT_MODULE_MOCKUP` / `SENTIENT_PAGE_MOCKUP`) permettent
  de remplacer les mockups (tests, templates d'équipe) — voir `internal/module/scaffold.go`

#### Sortie TUI

```
? Domaine du module (reverse-DNS) : com.example.blog-manager
? Identifiant du module (kebab-case) : blog-manager
? Nom de l'application : Blog Manager
? Version : 0.0.0
? Icône (lucide) : PuzzleIcon
? URL de page : blog-manager
? Description : Gestion de blog et d'articles

  ✓ Module créé : external_modules/com.example.blog-manager/
  ✓ Token généré : a1b2c3d4-e5f6-7890-abcd-ef1234567890
  ✓ manifest.json initialisé
  ✓ index.tsx initialisé
  ✓ Page : src/app/blog-manager/page.tsx

  Prochaines étapes :
    sentients connect
    sentients pack com.example.blog-manager
    sentients publish
```

---

### 5.3 `sentients connect`

#### Purpose

Authentifier le développeur avec son compte `sentient-connect` et stocker les credentials de
manière sécurisée.

#### Comportement

1. **Vérifier si déjà connecté** : credentials existantes dans le keychain
   - Si oui → afficher le statut et demander si reconnexion souhaitée
2. **Demander l'email** via input Bubbletea
3. **Demander le mot de passe** via input Bubbletea (masqué)
4. **Envoyer les credentials** à l'API `sentient-connect` (`POST /api/auth/sign-in`)
5. **Vérifier la réponse** :
   - **Succès sans MFA** → stocker le token Bearer + device dans le keychain
   - **MFA requis** (`mfaRequired: true`) → enchaîner sur l'étape MFA
6. **MFA** (si requis, endpoints gardés → le token de session doit être attaché en Bearer) :
   - Interroger les facteurs disponibles → `POST /api/mfa/challenge`
   - Selon le facteur choisi :
     - **TOTP** : demander le code 6 chiffres → `POST /api/mfa/totp/verify`
     - **Backup code** : demander le code → `POST /api/mfa/recovery/verify`
   - Valider → stocker le `mfa_token` retourné
7. **Afficher le résumé** : connecté en tant que `email`, rôle, organisation

#### Stockage des credentials

| Donnée | Emplacement | Chiffrement |
|--------|-------------|-------------|
| `access_token` | Keychain (`sentient-cli.access_token`) | Oui (keychain natif) |
| `mfa_token` | Keychain (`sentient-cli.mfa_token`) | Oui (keychain natif) |
| `device` | Keychain (`sentient-cli.device`) | Oui (keychain natif) |
| `expires_at` | Keychain (`sentient-cli.expires_at`) | Non (timestamp) |
| `user.email` | Keychain (`sentient-cli.user_email`) | Non |
| `user.id` | Keychain (`sentient-cli.user_id`) | Non |
| `mfa_secret` | Keychain (`sentient-cli.mfa_secret`) | Oui (keychain natif) |

#### Sécurité

- **Plus jamais** de credentials en clair sur disque
- Session **à jeton unique** : le token Bearer est rafraîchi à l'expiration — par `POST /oauth/token`
  (`grant_type=refresh_token`, rotation) quand la session provient du flux OAuth `sentients auth`,
  sinon par `POST /api/auth/sessions/refresh` (session legacy)
- Le `mfa_secret` (si TOTP enrollment local) est chiffré dans le keychain
- Après 5 échecs de connexion → temporaire (5 min) avec message clair
- **Mode CI / headless** : les prompts sont alimentés par `SENTIENT_CLI_CONNECT_EMAIL`,
  `SENTIENT_CLI_CONNECT_PASSWORD` et `SENTIENT_CLI_MFA_CODE` (même pattern que `SENTIENT_CLI_YES`)

#### Sortie TUI

```
? Email : dev@example.com
? Mot de passe : ********
  ⠋ Vérification des identifiants...

? Code MFA (TOTP) : 123456
  ⠋ Vérification du code...

  ✓ Connecté en tant que dev@example.com
    Rôle : Developer
    Organisation : Mon Organisation
    Token expire le : 2026-09-17 14:30:00 UTC
```

---

### 5.4 `sentients disconnect`

#### Purpose

Supprimer toutes les credentials stockées et déconnecter le développeur.

#### Comportement

1. **Vérifier si connecté** : credentials présentes dans le keychain
   - Si non connecté → message informatif, rien à faire
2. **Demander confirmation** ( Bubbletea confirm )
3. **Supprimer** toutes les entrées du keychain :
   - `sentient-cli.access_token`
   - `sentient-cli.mfa_token`
   - `sentient-cli.device`
   - `sentient-cli.expires_at`
   - `sentient-cli.user_email`
   - `sentient-cli.user_id`
   - `sentient-cli.mfa_secret`
   - `sentient-cli.oauth_refresh_token`
4. **Invalider le token** côté serveur (`POST /api/auth/logout`, best-effort)
5. **Afficher confirmation**

#### Sortie TUI

```
? Confirmer la déconnexion : Oui
  ✓ Déconnecté avec succès
    Toutes les credentials ont été supprimées.
```

---

### 5.5 `sentients pack`

#### Purpose

Construire le build d'un module et créer une archive `.SenMod` compressée.

#### Comportement

1. **Identifier le module** :
   - Si un argument `<module>` est fourni → l'utiliser
   - Sinon → lister les modules dans `external_modules/` via un sélecteur Bubbletea
2. **Vérifier l'existence** du module et de ses fichiers essentiels (`manifest.json`, `index.tsx`)
3. **Valider le `manifest.json`** (champs requis : `id`, `name`, `version`, `token`)
4. **Construire les chemins** :
   - Source module : `external_modules/<module>/`
   - Source page : `src/app/<module.uri>/` (le `uri` du manifeste, sans `/` initial)
   - Source assets : `public/assets/<module>/` (si existe)
   - Destination : `.sentients/build/`
5. **Créer l'archive ZIP** :
   - Nom : `<module>-<version>.SenMod` (le `.SenMod` est un ZIP renommé)
   - Contenu : dossiers `external_modules/<module>/` + `public/assets/<module>/` et `src/app/<module.uri>` (si existe)
   - Préfixe dans l'archive : `external_modules/<module>/` + `public/assets/<module>/` et `src/app/<module.uri>`
6. **Déplacer** l'archive vers `.sentients/build/`
7. **Afficher le résumé** : taille de l'archive, emplacement

#### Structure de l'archive `.SenMod`

```
<SenMod-file>.SenMod (ZIP)
├── external_modules/<module>/
│   ├── manifest.json
│   ├── index.tsx
│   ├── application/
│   ├── domain/
│   ├── infrastructure/
│   └── presentation/
├── src/app/<module.uri>/
│   └── page.tsx
└── public/assets/<module>/    (optionnel)
    └── ...
```

#### Contraintes

- Le dossier `.sentients/build/` est créé automatiquement s'il n'existe pas
- Si une archive du même nom existe → demander confirmation (écraser)
- Le `manifest.json` doit être valide avant le pack
- La taille maximale de l'archive est de 50 MB (limite store)

#### Sortie TUI

```
? Sélectionner le module : blog-manager
  ⠋ Validation du manifest.json...
  ⠋ Construction de l'archive...
  ⠋ Déplacement vers .sentients/build/

  ✓ Archive créée avec succès
    Module : blog-manager v0.1.0
    Fichier : .sentients/build/blog-manager-0.1.0.SenMod
    Taille : 12.4 KB
```

---

### 5.6 `sentients publish`

#### Purpose

Construire et publier un module dans le store via l'API `sentient-connect`.

#### Comportement

1. **Vérifier l'authentification** : token Bearer valide dans le keychain
   - Si non connecté → `sentients connect` automatique
2. **Identifier le module** : sélecteur Bubbletea si non fourni
3. **Vérifier le `manifest.json`** :
   - Si les métadonnées sont incomplètes (champs vides) → **demander** :
     - `name` : nom affiché du module
     - `description` : description courte
     - `publisher.id` : identifiant développeur
     - `publisher.name` : nom affiché du développeur
   - Proposer de mettre à jour le `manifest.json` local
4. **Exécuter `sentients pack`** en interne (construction de l'archive)
5. **Envoyer l'archive** à l'API developer-store (`sentient-api-connect`, §8.2 ;
   `docs/specs/applications/sentient-connect.md`) en 3 étapes :
   - Résoudre le **produit module** : réutiliser le produit lié (`manifest.token`) sinon le créer
     (`POST /api/developer-store/modules` — `{name, slug, type, primaryCategory, token,
     description, icon, secondaryCategory?}` ; catégorie primaire = `manifest.category`, repli `SYSTEM`)
   - Créer la **version** (`POST /api/developer-store/modules/:id/versions` —
     `{versionString, buildNumber, …}` ; `buildNumber` = dernière build + 1, résolue via
     `GET …/versions`)
   - **Déclarer l'artefact** (`POST /api/developer-store/modules/:id/versions/:versionId/artifact` —
     `manifest` JSON, `checksum` SHA-256 hex, `signature` base64 (.SenMod.sig), `size`)
   - Headers : `Authorization: Bearer <token>`
6. **Gérer la réponse** :
   - **Succès** → afficher l'URL du module dans le store
   - **Conflit** (version existante) → demander si bump de version souhaité
   - **Erreur** → afficher le message d'erreur détaillé
7. **Mettre à jour le `manifest.json`** local avec la version publiée et le token distant résolu

#### Contraintes

- L'authentification est **obligatoire**
- La version doit être supérieure à la dernière version publiée (SemVer)
- Le module doit passer l'audit (`sentients audit`) avant la publication
- Si l'audit échoue → proposer de corriger avant de publier

#### Sortie TUI

```
  ✓ Authentifié : dev@example.com
? Sélectionner le module : blog-manager

  Métadonnées du module :
    Nom : Blog Manager
    Description : Gestion de blog et d'articles
    Version : 0.1.0

? Confirmer la publication : Oui
  ⠋ Validation du manifest.json...
  ⠋ Construction de l'archive...
  ⠋ Publication sur le store...
  ✓ Publié avec succès

  Module : blog-manager v0.1.0
  URL : https://store.sentient.dev/modules/blog-manager
```

---

### 5.7 `sentients link`

#### Purpose

Lier un module créé dans `sentient-connect` avec le module en local, via son token produit.

#### Comportement

1. **Vérifier le contexte projet** (racine + `external_modules/`) et l'authentification (sinon → `sentients connect`)
2. **Sélectionner le module local** : argument positionnel ou sélecteur Bubbletea
3. **Lister les modules en ligne** via API `GET /api/developer-store/modules`, proposer une sélection
   (items au format `token — name vversion`)
4. **Résoudre le token distant** :
   - En mode CI (non-interactif) : `sentients link <module> <token>` en arguments
   - En interactif : choix dans la liste
5. **Valider le token** via `GET /api/developer-store/modules/<id>` (existe + appartient au développeur)
6. **Mettre à jour le `manifest.json` local** : `token` remplacé par le token distant, métadonnées
   distantes fusionnées dans les champs absents (`name`, `description`, `publisher.*`)
7. **Persister l'état** dans `.sentients/links.json` (`{"modules": {"<module>": "<token>"}}`) —
   source de vérité pour `unlink`/`LinkedModules`
8. **Afficher le résumé** : module lié (local ↔ distant)

#### Sortie TUI

```
? Sélectionner le module local : blog-manager
  ⠋ Récupération des modules en ligne…
? Token du module en ligne :
    m_abc123def456 — Blog Manager v0.1.0
  ⠋ Vérification du module distant…

  ✓ Module lié avec succès
    Local : external_modules/blog-manager/
    Distant : m_abc123def456 (Blog Manager v0.1.0)
```

---

### 5.8 `sentients unlink`

#### Purpose

Délier un module local de son correspondant dans `sentient-connect`.

#### Comportement

1. **Vérifier le contexte projet**
2. **Lister les modules localement liés** via `.sentients/links.json` (source de vérité) + les
   manifests portant un token non-UUID (migration)
3. **Sélectionner le module à délier** : argument positionnel ou sélecteur Bubbletea
4. **Afficher le lien actuel** (token + version distante)
5. **Optionnel** : `--sync-remote` synchronise d'abord les métadonnées locales vers le produit
   distant (`PUT /api/developer-store/modules/:id`, best-effort)
6. **Demander confirmation** (Bubbletea confirm ; non-interactif → exécution directe)
7. **Délier** : régénère un **nouveau token UUID local** dans `manifest.json` et retire l'entrée de
   `.sentients/links.json`
8. **Afficher confirmation**

#### Sortie TUI

```
? Sélectionner le module à délier : blog-manager
  Actuellement lié à : m_abc123def456 (Blog Manager v0.1.0)

? Confirmer la déliaison : Oui
  ✓ Module délié avec succès
    external_modules/blog-manager/ n'est plus lié à un module distant.
```

---

### 5.9 `sentients debug <module>`

#### Purpose

Lancer le debug d'un ou tous les modules dans `external_modules/` : validation puis build réel du module.

#### Comportement

1. **Analyser l'argument** :
   - Si `<module>` est fourni → debug uniquement ce module
   - Sinon → debug **tous** les modules dans `external_modules/`
2. **Valider le module** (mêmes règles que `audit`) : si erreurs → statut `ERROR` avec la liste
   des règles en échec
3. **Détecter le gestionnaire de paquets** (bun → pnpm → yarn → npm) ; aucun → statut `WARNING`
4. **Résoudre la commande de build** :
   - Script du `package.json` du module puis du projet (candidats `debug`, `dev`, `build`,
     comparés par clé exacte, pas de collision de sous-chaîne)
   - Repli : **type-check TypeScript réel** `tsc --noEmit` si le module contient des sources
     `.ts`/`.tsx` et qu'un `tsconfig.json` + un `tsc` résolvable existent
5. **Exécuter la commande** :
   - OK → statut `OK` (sortie affichée si présente)
   - Échec → statut `ERROR`, sortie d'erreur affichée
6. **Aucun build ni type-check possible** → statut `WARNING` (`no_build_script`), jamais un faux "OK"
7. **Mode all modules** : itérer sur chaque module et afficher le tableau de statut (nom, statut, erreurs)

#### Sortie TUI (single)

```
  Debug : blog-manager
  ⠋ Validation du module…
  ✓ Compilation réussie

  ┌─────────────────────────────────────────────┐
  │ [14:30:01] blog-manager: Module chargé       │
  │ [14:30:01] blog-manager: Routes enregistrées │
  │ [14:30:02] blog-manager: Aucune erreur       │
  └─────────────────────────────────────────────┘
```

#### Sortie TUI (all)

```
  Debug de tous les modules
  ┌──────────────────┬──────────┬─────────────┐
  │ Module           │ Statut   │ Erreurs     │
  ├──────────────────┼──────────┼─────────────┤
  │ blog-manager     │ ✓ OK     │ 0           │
  │ billing          │ ✓ OK     │ 0           │
  │ calendar         │ ⚠ 2      │ 2 warnings  │
  │ crm              │ ✓ OK     │ 0           │
  └──────────────────┴──────────┴─────────────┘
```

---

### 5.10 `sentients audit <module>`

#### Purpose

Auditer la conformité d'un ou tous les modules par rapport aux règles du système Sentient.

#### Comportement

1. **Analyser l'argument** :
   - Si `<module>` est fourni → audit uniquement ce module
   - Sinon → audit **tous** les modules dans `external_modules/`
2. **Vérifier l'existence** du ou des modules
3. **Exécuter les vérifications** (pour chaque module) :

#### Vérifications d'audit

| Catégorie | Règle | Sévérité |
|-----------|-------|----------|
| **manifest.json** | `lecture` du JSON valide | ERROR |
| **manifest.json** | Champ `id` présent et non vide | ERROR |
| **manifest.json** | Champ `name` présent et non vide | ERROR |
| **manifest.json** | Champ `version` au format SemVer valide | ERROR |
| **manifest.json** | Champ `token` UUID valide | ERROR |
| **manifest.json** | Champ `entry` pointe vers un fichier existant | ERROR |
| **manifest.json** | Champ `domain` au format `mod.sentients.<name>` | WARNING |
| **manifest.json** | Le `domain` correspond au dossier du module (`external_modules/<domain>`) | WARNING |
| **manifest.json** | `permissions` est un tableau (inspection JSON brut) | WARNING |
| **manifest.json** | `optionalRequirements` est présent (objet, `{}` admis) | WARNING |
| **manifest.json** | `platforms` est présent et `modes` est déclaré pour chaque plateforme `supported: true` | WARNING |
| **manifest.json** | `managerCompatibility` / `apiCompatibility` présents, plage `max` complète (`0.17.x`, pas `0.17.0`) | WARNING |
| **manifest.json** | `capabilities` est présent | WARNING |
| **manifest.json** | `category` appartient à l'enum `ModuleCategory` (si présent) | WARNING |
| **manifest.json** | `publisher` est présent (`id` + `name`) | WARNING |
| **index.tsx** | Fichier existe et exporte une valeur par défaut | ERROR |
| **index.tsx** | Déclaration module présente (`identifier` + `widgets`) — déclaration déclarative, l'ancien `render` async n'existe plus | ERROR |
| **Clean Architecture** | Les composants n'importent pas directement les services (`../services`, `application/service`) | ERROR |
| **Clean Architecture** | Les services ne contiennent pas de JSX (`services/` et `application/service/`, heuristique regex JSX) | ERROR |
| **requirements** | Les requirements listées existent dans `external_modules/` **ou** `src/modules/` (exceptions : modules core plateforme `organization`, `identity`) | ERROR |
| **dependencies** | Les dépendances npm listées sont installées dans `node_modules` | ERROR |
| **assets** | `public/assets/<module>/` contient des fichiers (si le dossier existe) | WARNING |

> Les champs contrôlés (`id`, `name`, `version`, `token`, `entry`, `domain`, `permissions`) font
> partie du contrat canonique du manifeste (`docs/modules/module-manifest.md`). La conformité
> **complète** au schéma (`module-manifest.schema.json`, 24 champs requis, plateformes,
> compatibilité, capacités, prérequis, dépendances…) est validée en CI par le SDK, pas par
> l'audit CLI (qui reste une heuristique locale).

> Note : la règle « pas de dépendances en double » a été retirée (itération d'une map — les clés
> dupliquées sont impossibles par construction). La règle « pas de logique métier dans les hooks »
> n'existe plus : le layout canonique n'a pas de dossier `hooks/`.

4. **Générer le rapport** :
   - Format par défaut : tableau TUI, coloré par sévérité (rouge = ERROR, orange = WARNING, vert = OK),
     résumé « X erreurs, Y warnings »
   - `--output json` : machine-readable `{"modules": [{"module", "findings": [{category, rule, severity, message}]}]}`
   - `--output table` : forçage du tableau (non-interactif)

#### Sortie TUI

```
  Audit : blog-manager
  ┌──────────────────────┬──────────┬──────────────────────────┐
  │ Catégorie            │ Règle    │ Statut                   │
  ├──────────────────────┼──────────┼──────────────────────────┤
  │ manifest.json        │ id       │ ✓ Présent                │
  │ manifest.json        │ name     │ ✓ Présent                │
  │ manifest.json        │ version  │ ✓ SemValide              │
  │ manifest.json        │ token    │ ✓ UUID valide            │
  │ manifest.json        │ entry    │ ✓ Fichier existe         │
  │ index.tsx            │ export   │ ✓ Défaut exporté         │
  │ index.tsx            │ decl     │ ✓ Déclaration présente   │
  │ requirements         │ exist    │ ⚠ organization non trouvé│
  │ dependencies         │ install  │ ✓ Toutes installées      │
  │ Clean Architecture   │ services→JSX │ ✓ Conforme           │
  └──────────────────────┴──────────┴──────────────────────────┘

  Résumé : 0 erreurs, 1 warning
```

---

### 5.11 `sentients help`

#### Purpose

Afficher l'aide contextuelle de la CLI.

#### Comportement

1. **Sans argument** : afficher la liste de toutes les commandes avec descriptions
2. **Avec une commande** : afficher l'aide détaillée de cette commande (flags, exemples)

#### Sortie TUI (sans argument)

```
┌───────────────┐
│ ⬢ sentients   │   ← wordmark (badge brand, dégrade en texte sous --no-color)
└───────────────┘

Sentient CLI — Development tool for Sentient modules

Usage:
  sentients [command]

Available Commands:
  audit         Audit a module's conformance
  auth          Authenticate via OAuth2 (browser)
  connect       Connect to Sentient Connect
  create        Create a new module
  debug         Debug a module
  disconnect    Disconnect from Sentient Connect
  init          Initialize a new Sentient project
  link          Link a local module to a remote module
  pack          Pack a module
  publish       Publish a module to the store
  sign          Sign a module archive
  unlink        Unlink a local module from sentient-connect
  help          Help about any command

Flags:
      --help     help for sentients
  -v, --version  version for sentients

Global Flags:
      --lang string    language / UI locale (fr-FR, en-US, …)
      --no-color       disable colors
      --verbose        enable verbose logs

Use "sentients [command] --help" for more information about a command.

Exemples:
  sentients init
  sentients create module
  sentients connect
  sentients pack blog-manager
  sentients sign blog-manager
  sentients publish blog-manager
  sentients audit
```

> Le help est thématisé : labels de section teintés (accent), wordmark brand en tête ;
> les informations et messages — y compris l'aide — sont localisés (NFR-007).

---

### 5.12 `sentients -v` / `sentients --version`

#### Purpose

Afficher la version actuelle de la CLI.

#### Comportement

1. Lire la version compilée dans le binaire (via `ldflags` : `main.version`, `main.commit`, `main.date`)
2. Afficher : `sentients v<version> (<os>/<arch>) <commit>` (template de version Cobra)

#### Sortie

```
sentients v0.6.0 (darwin/arm64) abc1234
```

---

### 5.13 `sentients sign`

#### Purpose

Gérer les signatures numériques Ed25519 des modules : générer des clés, signer les archives `.SenMod`
et vérifier les signatures. La signature garantit l'intégrité et l'authenticité des modules
avant publication.

#### Sous-commandes

| Sous-commande | Description |
|---------------|-------------|
| `sentients sign keygen` | Générer une paire de clés Ed25519 et la stocker dans le keychain |
| `sentients sign <module>` | Signer l'archive `.SenMod` d'un module |
| `sentients sign verify <module>` | Vérifier la signature d'un module |

---

##### 5.13.1 `sentients sign keygen`

###### Comportement

1. **Vérifier si des clés existent déjà** dans le keychain
   - Si oui → afficher le fingerprint de la clé publique et demander régénération
2. **Générer une paire de clés Ed25519** (`crypto/ed25519`)
3. **Stocker** la clé privée et la clé publique dans le keychain système
   - Clé privée : `sentient-cli-signing.signing_private_key`
   - Clé publique : `sentient-cli-signing.signing_public_key`
   - Fallback : fichier chiffré `~/.sentient-cli/signing.enc` (AES-256-GCM)
4. **Afficher** le fingerprint SHA-256 de la clé publique (hex 64 caractères)

###### Contraintes

- La clé privée n'est **jamais** affichée à l'écran
- Si des clés existent déjà, la régénération écrase les précédentes après confirmation
- En mode non interactif, régénère silencieusement (pour CI/CD)

###### Sortie TUI

```
  ✓ Paire de clés Ed25519 générée avec succès
    Fingerprint : a1b2c3d4e5f6... (SHA-256 de la clé publique)
    Clé privée  : stockée dans le keychain système
    Clé publique : stockée dans le keychain système
```

---

##### 5.13.2 `sentients sign <module>`

###### Comportement

1. **Vérifier le contexte** : être à la racine d'un projet Sentient
2. **Identifier le module** : argument `<module>` ou sélecteur Bubbletea
3. **Charger le `manifest.json`** du module pour obtenir la version
4. **Vérifier que l'archive `.SenMod` existe** dans `.sentients/build/`
   - Si absente → erreur avec suggestion d'exécuter `sentients pack <module>`
5. **Charger la clé privée** depuis le keychain
   - Si absente → erreur avec suggestion d'exécuter `sentients sign keygen`
6. **Signer l'archive** :
   - Lire le contenu de l'archive `.SenMod`
   - Signer avec `ed25519.Sign(privateKey, archiveData)`
   - Écrire la signature dans `<archive>.sig` (même dossier que l'archive)
7. **Afficher le résumé** : module, version, fingerprint du signataire, chemin du `.sig`

###### Contraintes

- L'archive `.SenMod` doit exister (résultat de `sentients pack`)
- La clé privée doit exister dans le keychain
- Si un fichier `.sig` existe déjà pour cette archive → demander confirmation (écraser)
- Le fichier `.sig` est un binaire contenant uniquement la signature Ed25519 (64 octets)

###### Sortie TUI

```
? Sélectionner le module : blog-manager
  ⠋ Chargement de la clé de signature…
  ⠋ Signature de l'archive…

  ✓ Archive signée avec succès
    Module   : blog-manager v0.1.0
    Archive  : .sentients/build/blog-manager-0.1.0.SenMod
    Signature : .sentients/build/blog-manager-0.1.0.SenMod.sig
    Signataire : a1b2c3d4... (fingerprint SHA-256)
```

---

##### 5.13.3 `sentients sign verify <module>`

###### Comportement

1. **Vérifier le contexte** : être à la racine d'un projet Sentient
2. **Identifier le module** : argument `<module>` ou sélecteur Bubbletea
3. **Charger le `manifest.json`** du module pour obtenir la version
4. **Vérifier que l'archive `.SenMod` et le fichier `.sig` existent**
5. **Charger la clé publique** depuis le keychain
   - Si absente → erreur avec suggestion d'exécuter `sentients sign keygen`
6. **Vérifier la signature** :
   - Lire l'archive `.SenMod` et le fichier `.sig`
   - Vérifier avec `ed25519.Verify(publicKey, archiveData, signature)`
7. **Afficher le résultat** : ✓ Signature valide ou ✗ Signature invalide

###### Contraintes

- L'archive `.SenMod` ET le fichier `.sig` doivent exister
- La clé publique doit exister dans le keychain
- En cas de signature invalide → afficher un message d'erreur explicite (possiblement archive corrompue ou clé incorrecte)

###### Sortie TUI (valide)

```
? Sélectionner le module : blog-manager
  ⠋ Vérification de la signature…

  ✓ Signature valide
    Module    : blog-manager v0.1.0
    Archive   : .sentients/build/blog-manager-0.1.0.SenMod
    Signataire : a1b2c3d4...
```

###### Sortie TUI (invalide)

```
? Sélectionner le module : blog-manager
  ⠋ Vérification de la signature…

  ✗ Signature invalide
    Module   : blog-manager v0.1.0
    Archive  : .sentients/build/blog-manager-0.1.0.SenMod
    → L'archive a pu être modifiée ou la clé de vérification est incorrecte.
```

---

### 5.14 `sentients auth`

#### Purpose

Authentifier le développeur via le flux OAuth2 **code d'autorisation + PKCE** (RFC 7636),
en passant par le navigateur. Complément du `sentients connect` (email/mot de passe), le
flux ouvre la page d'autorisation de `sentient-auth`, reçoit la redirection sur un serveur
local en boucle, échange le code contre des jetons, puis stocke la session de façon
sécurisée.

#### Comportement

1. **Vérifier si déjà connecté** : credentials existantes dans le keychain — afficher le
   statut et proposer la reconnexion (comme `connect`)
2. **Résoudre la configuration OAuth** depuis l'entrée `oauth` de `sentient-auth` dans
   `app.config.json` : `authorizationEndpoint`, `tokenEndpoint`, `revokeEndpoint`,
   `clientId`, `scopes` (défauts : `/oauth/authorize`, `/oauth/token`, `/oauth/revoke`,
   client `sentient-cli`, scopes `openid profile email`)
3. **Générer le PKCE** : `code_verifier` (43–128 caractères base64url) + `code_challenge`
   S256, et un `state` aléatoire (anti-CSRF)
4. **Ouvrir le navigateur** sur l'URL d'autorisation :
   `GET <baseUrl><authorizationEndpoint>?response_type=code&client_id=…&redirect_uri=…&scope=…&code_challenge=…&code_challenge_method=S256&state=…`
   — l'URL est affichée dans le terminal (recopiable si le navigateur ne s'ouvre pas)
5. **Recevoir la redirection** sur un serveur HTTP local en boucle
   (`http://127.0.0.1:<port>/callback`, port éphémère) ; extraire `code` + `state` et
   vérifier le `state`
6. **Échanger le code** au point d'entrée token (POST `application/x-www-form-urlencoded`) :
   `grant_type=authorization_code&code=…&redirect_uri=…&client_id=…&code_verifier=…`
7. **Stocker la session** : `access_token`, `refresh_token` (si présent) et expiration
   (`expires_in`), dans le keychain (repli vault chiffré)
8. **Afficher le résumé** : type de jeton, expiration, scope, présence d'un refresh token

#### Mode non interactif (CI / headless)

Le code d'autorisation est fourni via la variable d'environnement `SENTIENT_CLI_AUTH_CODE`
(même pattern que `SENTIENT_CLI_YES`) : la CLI saute l'étape navigateur + serveur local et
échange directement le code. Sans code en mode non interactif, la commande échoue avec une
erreur catégorisée (exit 2).

#### Sortie TUI

```
  Ouverture de la page d'autorisation dans votre navigateur…
  https://auth.sentient.protorians.com/oauth/authorize?response_type=code&…

  ⠋ Échange du code d'autorisation…

  ✓ Authentifié via OAuth2
    Type de jeton : Bearer
    Portée        : openid profile email
    Expiration du jeton : 2026-09-17 14:30:00 UTC
```

#### Contraintes

- Le `state` renvoyé par le serveur doit correspondre à celui généré (anti-CSRF) — sinon
  erreur catégorisée
- Les jetons ne sont **jamais** en clair sur disque : ils sont stockés dans le keychain
  (repli vault chiffré `credentials.enc`)
- Le `redirect_uri` est toujours un loopback `http://127.0.0.1:<port>/callback` (client
  public natif, PKCE `S256` obligatoire)

---

## 6. Modèle de données local

### 6.1 Fichier `sentients.config.json` (optionnel)

Placé à la racine du projet Sentient, ce fichier permet de configurer la CLI.

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
  },
  "cli": {
    "lang": "fr-FR"
  }
}
```

> La configuration est **JSON uniquement** (le parser TOML a été retiré en 0.0.9). Le fichier
> historique `sentient.config.toml` ne sert plus que de marqueur de projet (racine) pour
> `create`/`link`/etc., et `sentients.config.json` est écrit par `sentients init`.
> `cli.lang` force la langue d'interface (NFR-007, FR-025) ; un champ vide garde l'auto-détection
> (`SENTIENT_CLI_LANG` / locale OS).

### 6.2 Fichier `manifest.json` (par module)

Le `manifest.json` est le **contrat technique déclaratif** de chaque module (identité, plateformes,
compatibilité, permissions/scopes, capacités, activation, prérequis, dépendances npm, interface).
C'est la source de vérité dont découlent la déclaration React (`index.tsx`,
`ModuleDeclarationInterface`) et le catalogue backend.

- **Schéma canonique** : `schemaVersion: 1`, publié avec le SDK
  (`@sentients/sdk`, `docs/modules/module-manifest.md`), validé en CI. La structure complète
  est décrite en §5.2 (`sentients create module`).
- **Identité stable** : `id`, `domain`, `key`, `name`, `permissions` ne changent jamais (le
  catalogue, les activations et les contrôles d'accès en dépendent) ; `version` ne bouge que par
  incrément SemVer.
- **Séparation** : `requirements`, `optionalRequirements`, `dependencies`, `devDependencies` sont
  déclarés **uniquement** ici (jamais dans `index.tsx`).
- **`token`** : identifiant public de liaison produit (UUID local régénéré au `unlink`, remplacé
  par l'id produit distant au `link`) — ce n'est pas un secret.

### 6.3 Keychain — Hiérarchie des clés

```
sentient-cli/
├── access_token      # Token Bearer JWT (session à jeton unique)
├── mfa_token         # Token MFA court (après vérification TOTP/recovery)
├── device            # ID du device (session connectée)
├── expires_at        # Timestamp d'expiration
├── user_id           # ID du développeur
├── user_email        # Email du développeur
├── mfa_secret        # Secret TOTP (si enrollment local)
└── oauth_refresh_token  # Refresh token OAuth2 (flux `sentients auth`)

sentient-cli-signing/
├── signing_public_key   # Clé publique Ed25519 (fingerprint du développeur)
└── signing_private_key  # Clé privée Ed25519 (signature des archives .SenMod)
```

---

## 7. Sécurité

### 7.1 Stockage des credentials

| Mécanisme | Plateforme | Implémentation |
|-----------|------------|----------------|
| macOS Keychain | macOS | `security` CLI ou `go-keyring` |
| Secret Service | Linux | D-Bus + `libsecret` via `go-keyring` |
| Credential Manager | Windows | `cmdkey` ou `Credential Manager` via `go-keyring` |
| Fichier chiffré (fallback) | Sans keychain | Vault AES-256-GCM `~/.sentient-cli/credentials.enc` |

- Le backend par défaut est le keychain système ; si le keychain est injoignable (probe de lecture),
  la CLI bascule **transparentement** sur un vault fichier chiffré AES-256-GCM
  (`credentials.enc` pour l'auth, `signing.enc` pour les clés).
- La clé AES du vault est dérivée en **PBKDF2** du secret machine par utilisateur
  (`~/.sentient-cli/machine.secret`) — jamais de passphrase codée en dur.
- `SENTIENT_CLI_STORE=keychain|file` force le backend (CI/headless) ; `NewStoreVolatile` isole les tests.
- Service keychain : `sentient-cli` (credentials), `sentient-cli-signing` (clés de signature).

### 7.2 Chiffrement des archives

- Les archives `.SenMod` ne contiennent **jamais** de credentials, tokens ou données sensibles
- Le `manifest.json` ne contient que les métadonnées publiques du module
- Le token UUID est un identifiant public, pas un secret

### 7.3 Communication réseau

- Toutes les communications avec `sentient-connect` utilisent **HTTPS** (TLS 1.3)
- Les tokens sont transmis via le header `Authorization: Bearer <token>`
- Pas de credentials dans les query parameters
- Validation SSL stricte (pas de `--insecure`)

### 7.4 Rate limiting

| Action | Limite | Durée |
|--------|--------|-------|
| Tentative de connexion | 5 | 5 min |
| Publication | 10 | 1 heure |
| Appels API | 100 | 1 minute |

### 7.5 MFA

- Si le compte développeur a la MFA activée, `sentients connect` **exige** la vérification
- Le secret TOTP peut être géré côté serveur (recommandé) ou stocké localement (optionnel)
- Les backup codes sont utilisables uniquement en secours

### 7.6 Signature numérique

- Les clés de signature sont stockées dans le keychain OS (service `sentient-cli-signing`)
- La clé privée n'est **jamais** affichée à l'écran ni exportée
- Fallback : fichier chiffré `~/.sentient-cli/signing.enc` (AES-256-GCM, clé PBKDF2 du secret machine)
  quand le keychain n'est pas disponible
- L'algorithme utilisé est **Ed25519** (signatures compactes de 64 octets, clés de 32 octets)
- Les fichiers `.sig` sont des binaires contenant uniquement la signature Ed25519
- La vérification de signature utilise la clé publique stockée dans le keychain
- En cas de perte de clés, `sentients sign keygen` permet de régénérer une nouvelle paire

---

## 8. Communication API

> La CLI consomme deux backends du workspace (`docs/specs/applications/module-distribution.md`) :
> `sentient-api-core` (authentification, MFA, OAuth — port `5711`) et `sentient-api-connect`
> (**Developer Store** de publication — port `5721`). Le **catalogue public** vit dans un troisième
> service, `sentient-api-store` (port `5731`, `/api/catalog/*`), non appelé directement par la CLI.
>
> Tous les endpoints sont servis derrière un préfixe global **`/api`** (hors routes OAuth publiques)
> et une enveloppe Raiton unique : `{ message, data, statusCode }`
> (`RaitonResponses(message, data, statusCode)`). Les erreurs reprennent l'enveloppe (`message`), le
> code HTTP et un `code` optionnel.
>
> La base URL du client est résolue par le **registre `app.config.json`** (TECH-009) : le binaire
> embarque le registre workspace (`sentient-auth`, `sentient-store`, `sentient-connect`, …), un
> `app.config.json` local peut le surcharger, et `SENTIENT_AUTH_API` force la base URL (priorité max).
> Le timeout HTTP par défaut est de 30 s (surchargeable par application via `api.timeout`). Le header
> `Authorization: Bearer <token>` est posé à chaque requête quand une session existe.

### 8.1 Endpoints `sentient-api-core` — authentification, MFA, OAuth

| Méthode | Chemin | Description |
|---------|--------|-------------|
| POST | `/api/auth/sign-in` | Authentification (email + password) → `{user, token, device}` |
| POST | `/api/auth/logout` | Déconnexion (invalidation token, gardé) |
| POST | `/api/auth/sessions/refresh` | Rafraîchissement du token (gardé) |
| POST | `/api/mfa/challenge` | Défi MFA (gardé) → `{mfaRequired, challenge?, factors}` |
| POST | `/api/mfa/totp/verify` | Vérification code TOTP (gardé) → `{mfaVerified, mfaToken?}` |
| POST | `/api/mfa/recovery/verify` | Vérification backup code (gardé) |
| GET | `/oauth/authorize` | Autorisation OAuth2 (navigateur) — `response_type=code`, PKCE S256, `state` |
| POST | `/oauth/token` | Échange de code / refresh OAuth2 (`authorization_code` / `refresh_token`, form-encoded) |
| POST | `/oauth/revoke` | Révocation d'un token OAuth2 |

> Le module OAuth (`docs/specs/applications/sentient-oauth.md`) expose aussi `client_credentials`,
> `refresh_token` (rotation), `/oauth/introspect`, `/oauth/userinfo` (OIDC), `/.well-known/*` et
> l'administration des clients ; la CLI n'utilise que le sous-ensemble **code + PKCE** ci-dessus.

### 8.2 Endpoints `sentient-api-connect` — Developer Store (publication)

| Méthode | Chemin | Description |
|---------|--------|-------------|
| GET | `/api/developer-store/modules` | Liste des produits module (`DeveloperModule`) du développeur (tableau brut ou paginé `{items, …}`) |
| GET | `/api/developer-store/modules/:id` | Détail d'un produit module (id) |
| GET | `/api/developer-store/modules/:id/versions` | Liste des versions publiées (meilleure version pour link/publish) |
| POST | `/api/developer-store/modules` | Création d'un produit module |
| PUT | `/api/developer-store/modules/:id` | Mise à jour des métadonnées du produit (unlink `--sync-remote`) |
| POST | `/api/developer-store/modules/:id/versions` | Création d'une version |
| POST | `/api/developer-store/modules/:id/versions/:versionId/artifact` | Déclaration de l'artefact |

> Modèles `DeveloperModule` / `DeveloperModuleVersion` / `DeveloperModuleArtifact` (anciens
> `StoreModule*`) ; jeton `Bearer` partagé émis par `sentient-api-core` (`JWT_SECRET` aligné). Voir
> `docs/specs/applications/sentient-connect.md`.

### 8.3 DTOs

#### POST `/api/auth/sign-in` — `SignInRequest`

| Champ | Type | Requis | Description |
|-------|------|--------|-------------|
| `email` | string | oui | Email du développeur |
| `password` | string | oui | Mot de passe |

#### Réponse `SignInResponse` (succès — `SignInVm`)

| Champ | Type | Description |
|-------|------|-------------|
| `user` | object | `{ id, username, email?, avatar?, status, roles[] }` |
| `token` | string | JWT Bearer unique (session 24 h, TTL estimé côté CLI) |
| `device` | string | UUID de l'appareil (session) |

Pas de `expires_in` ni de `refresh_token` : la CLI estime l'expiration à 24 h et
rafraîchit via `POST /api/auth/sessions/refresh`.

#### POST `/api/developer-store/modules` — `CreateModuleProductDto`

| Champ | Type | Requis | Description |
|-------|------|--------|-------------|
| `name` | string | oui | Nom affiché (2-120) |
| `slug` | string | oui | Identifiant unique par compte développeur (kebab-case) |
| `type` | enum | non | `DeveloperModuleType` (`WEB_APP_REMOTE` par défaut, …) |
| `primaryCategory` | string | oui | Catégorie storefront |
| `secondaryCategory` | string | non | Catégorie secondaire |
| `token` | string | non | 3-64 ; **généré (UUID) si omis** — clé de réutilisation produit par la CLI |
| `description` / `icon` | string | non | Description / icône de la fiche produit |

> La CLI enregistre le `token` produit dans le `manifest.json` local et le réutilise pour retrouver
> l'entrée de publication (idempotence du parcours `publish`).

#### POST `/api/developer-store/modules/:id/versions` — `CreateVersionRequest`

| Champ | Type | Requis | Description |
|-------|------|--------|-------------|
| `versionString` | string | oui | Version SemVer du manifest, **strictement supérieure** à la dernière (sinon `409`) |
| `buildNumber` | number | non | Numéro de build (défaut : dernière build + 1) |
| `releaseNotes` | JSON | non | Notes de release (objet, par langue) |
| `minManager` / `maxManager` | string | non | Compatibilité manager (`manifest.managerCompatibility`) |
| `minApi` / `maxApi` | string | non | Compatibilité API (`manifest.apiCompatibility`) |
| `supportedRuntimes` | string[] | non | Runtimes activés du manifest (défaut `["bun"]`) |

#### POST `/api/developer-store/modules/:id/versions/:versionId/artifact` — `DeclareArtifactRequest`

| Champ | Type | Requis | Description |
|-------|------|--------|-------------|
| `manifest` | JSON | oui | Contenu du `manifest.json` |
| `checksum` | string | oui | SHA-256 hex de l'archive `.SenMod` |
| `signature` | string | non | Signature Ed25519 (base64 du `.SenMod.sig`, vide si non signé) |
| `size` | number | non | Taille de l'archive en octets (défaut 0) |
| `storageKey` | string | non | Clé de stockage blob (téléversement binaire — réservé) |

---

## 9. TUI — Composants Bubbletea

### 9.1 Priorité d'utilisation des composants

> **Règle obligatoire** : avant de créer tout composant TUI custom, **prioriser
> systématiquement les composants existants de l'écosystème Bubbletea / Bubbles**
> (https://github.com/charmbracelet/bubbletea/tree/main/examples,
> https://github.com/charmbracelet/bubbles). Un composant custom ne doit être
> implémenté que si aucun composant bubbles existant ne correspond au besoin, ou
> si le composant bubbles ne peut pas être étendu/overridé pour le cas d'usage.
>
> **Ordre de priorité** :
> 1. Composants `bubbles/*` natifs (spinner, textinput, list, table, viewport, confirm, pager, filepicker, progress, help, key, stopwatch, textarea…)
> 2. Extension / styling des composants bubbles via lipgloss (styles custom, delegates)
> 3. Composition de plusieurs composants bubbles dans un modèle Bubbletea `Model`
> 4. Composant custom uniquement en dernier recours, avec justification documentée
>
> **Vérification avant implémentation** : consulter les examples officiels
> (https://github.com/charmbracelet/bubbletea/tree/main/examples) et la
> documentation bubbles (https://github.com/charmbracelet/bubbles#readme) pour
> chaque nouveau composant TUI.

### 9.2 Composants réutilisables

| Composant | Usage | Bibliothèque |
|-----------|-------|--------------|
| `RunWithSpinner` | Indicateur de progression (`tui/spinner.go`) | `bubbles/spinner` |
| `RunWithProgress` | Barre de progression (téléchargements release, `tui/progress.go`) | `bubbles/progress` |
| `Select` | Sélection dans une liste (`tui.Select`) | `bubbles/list` |
| `AskText` | Saisie de texte (`tui.AskText`) | `bubbles/textinput` |
| `Confirm` | Confirmation oui/non (`tui.Confirm`) | Custom (modèle Bubbletea minimal) |
| `Table` | Affichage de données tabulaires (`tui.Table`, arrondi + bandes) | Custom (lipgloss) |
| `SummaryCard` / `Wordmark` / `StepsList` / `LogsBlock` | Cartes de synthèse, badge brand, listes d'étapes, blocs de logs (`tui/components.go`) | Custom (lipgloss) |

> Tous les composants interactifs se **dégradent en sortie non-interactive** lorsque le terminal
> n'est pas un TTY (ci / pipes) : prompts résolus via `SENTIENT_CLI_YES`, arguments positionnels,
> texte sur stderr. `Table` est un composant statique custom (pas de sélection) ; les sélections
> de liste passent par `bubbles/list`.

### 9.3 Styles

La palette est une identité **brand sage/olive à deux arrêts** (tous les éléments sémantiques
partagent l'arrêt du thème) :

- `#C1A875` — **sage**, l'accent principal (lisibilité sur fond sombre)
- `#725B2A` — **olive sombre**, l'arrêt secondaire (encre foncée sur fond clair)

| Élément | Couleur (dark) | Couleur (light) |
|---------|---------------|-----------------|
| Succès / Erreur / Warning / Info / Accent | `#C1A875` (sage) | `#725B2A` (olive) |
| Muted / Texte / Bordure | `#C1A875` (sage) | `#725B2A` (olive) |
| Soft (surfaces tintées, bandes, chips) | `#725B2A` (olive) | `#C1A875` (sage) |

### 9.4 Thème

La CLI détecte automatiquement le thème du terminal (dark/light) via `lipgloss.HasDarkBackground()`
et adapte les couleurs en conséquence.

---

## 10. Build & Distribution

### 10.1 GoReleaser

```yaml
# .goreleaser.yaml
version: 2
before:
  hooks:
    - go mod tidy
    - go test ./...

builds:
  - main: .
    binary: sentients
    env:
      - CGO_ENABLED=0
    goos:
      - linux
      - darwin
      - windows
    goarch:
      - amd64
      - arm64
    ignore:
      - goos: windows
        goarch: arm64      # Windows arm64 non livré (NFR-003)
    ldflags:
      - -s -w
      - -X main.version={{.Version}}
      - -X main.commit={{.Commit}}
      - -X main.date={{.Date}}

archives:
  - id: packages
    formats:
      - tar.gz
    name_template: sentients-cli_{{ .Version }}_{{ .Os }}_{{ .Arch }}
    format_overrides:
      - goos: windows
        formats:
          - zip
  - id: binaries
    formats:
      - binary
    name_template: sentients_{{ .Version }}_{{ .Os }}_{{ .Arch }}

checksum:
  name_template: checksums.txt

snapshot:
  version_template: "{{ incpatch .Version }}-next"

changelog:
  sort: asc
  filters:
    exclude:
      - "^docs:"
      - "^test:"
```

### 10.2 Installation

```bash
# Go install (releases GitHub)
go install github.com/protorians/sentient-cli@latest

# npm / npx (npmjs)
npm install -g @sentients/cli
# ou
npx @sentients/cli

# macOS / Linux
curl -sSL https://get.sentient.dev/cli | sh

# Windows (PowerShell)
iwr -useb https://get.sentient.dev/cli.ps1 | iex

# Homebrew (à créer)
brew install protorians/sentient/sentient-cli
```

### 10.3 Variables de compilation

| Variable | Description | Défaut |
|----------|-------------|--------|
| `main.version` | Version du binaire | `dev` |
| `main.commit` | Hash du commit git | `none` |
| `main.date` | Date de compilation | compilation time |

---

## 11. Gestion des erreurs

### 11.1 Codes de sortie

| Code | Signification |
|------|---------------|
| `0` | Succès |
| `1` | Erreur générique |
| `2` | Erreur d'authentification |
| `3` | Module non trouvé |
| `4` | Manifest invalide |
| `5` | Erreur réseau |
| `6` | Erreur de permission |
| `7` | MFA requis / échoué |
| `10` | Erreur de build |
| `11` | Erreur de publication |
| `12` | Erreur de signature numérique |

### 11.2 Messages d'erreur

Les messages sont **localisés** (NFR-007, FR-025) : catalogues `en-US` (défaut) et `fr-FR`
embarqués dans le binaire, résolution `--lang` → `SENTIENT_CLI_LANG` → `cli.lang` → locale OS
(`LC_ALL` / `LC_MESSAGES` / `LANG`) → repli `en-US`. Tous les textes (messages, prompts, help,
erreurs) passent par `i18n.T`/`i18n.Tf`.

Les erreurs catégorisées sont rendues sur **stderr** dans une carte encadrée (arrondi, teinte
erreur) :

```
✗ <Catégorie> : <Message détaillé>
  → <Action corrective suggérée>
```

Exemple :

```
✗ Authentication : Token expired
  → Run 'sentients connect' to sign in again.
```

---

## 12. Tests

### 12.1 Stratégie de test

| Type | Outil | Couverture cible |
|------|-------|------------------|
| Unit | `testing` stdlib (pas de testify) | 80% |
| Integration | `testing` + `testscript` | Scénarios complets |
| E2E | `testscript` (txtar) contre une **mock API** `httptest` (`e2e/mockapi`) | Toutes les commandes |

L'harness E2E (`e2e/e2e_test.go`) compile le binaire à partir de la racine, expose la CLI sous
`$SENTIENTS`, pointe `SENTIENT_AUTH_API` vers la mock API (une par script), force le vault fichier
(`SENTIENT_CLI_STORE=file`), désactive l'update check (`SENTIENT_CLI_SKIP_UPDATE=1`), injecte des
fixtures portables `bun/npm/tsc/node` et donne un `HOME` isolé writable par script.

### 12.2 Scénarios de test critiques

Suite E2E réelle (11 scripts txtar) : `01_help_version`, `02_init`, `02b_init_busy`,
`03_create`, `04_pack`, `05_sign`, `06_debug`, `07_audit`, `08_network`, `09_mfa`,
`10_link_unlink`, `11_auth`.

| ID | Scénario |
|----|----------|
| TC-001 | `sentients init` avec bun détecté |
| TC-002 | `sentients init` avec aucun gestionnaire détecté |
| TC-003 | `sentients create module` avec nom invalide |
| TC-004 | `sentients create module` avec nom valide |
| TC-005 | `sentients connect` succès sans MFA |
| TC-006 | `sentients connect` avec MFA TOTP |
| TC-007 | `sentients connect` échec (mauvais identifiants) |
| TC-008 | `sentients disconnect` avec confirmation |
| TC-009 | `sentients pack` module existant |
| TC-010 | `sentients pack` module avec assets |
| TC-011 | `sentients publish` succès |
| TC-012 | `sentients publish` version existante |
| TC-013 | `sentients link` succès |
| TC-014 | `sentients unlink` succès |
| TC-015 | `sentients debug` module unique |
| TC-016 | `sentients debug` tous les modules |
| TC-017 | `sentients audit` module conforme |
| TC-018 | `sentients audit` module avec erreurs |
| TC-019 | `sentients help` sans argument |
| TC-020 | `sentients help` avec commande |
| TC-021 | `sentients -v` affiche la version |
| TC-022 | `sentients sign keygen` génère et stocke les clés Ed25519 |
| TC-023 | `sentients sign <module>` signe l'archive `.SenMod` et produit un `.sig` |
| TC-024 | `sentients sign verify <module>` vérifie une signature valide |
| TC-025 | `sentients sign verify <module>` échoue sur archive modifiée ou signature invalide |
| TC-026 | `sentients auth` échange un code d'autorisation (OAuth2 + PKCE) et stocke la session |
| TC-027 | `sentients auth` en mode non-interactif sans `SENTIENT_CLI_AUTH_CODE` → erreur catégorisée (exit 2) |

---

## 13. Roadmap — Découpage Produit

> **État (2026-09-12)** : les Epics E-001 → E-008 sont largement implémentés dans les releases
> 0.0.8/0.0.9 (branche `alpha`). Le détail de l'alignement spec ↔ roadmap est dans
> `docs/rapport-implementation.md` (§5).

```
Product: sentient-cli v1.0.0
│
├── Release 0.1.0 (MVP)
│   │
│   ├── Epic E-001 : Initialisation & Création
│   │   ├── Story S-001 : `sentients init` (clone + deps)
│   │   └── Story S-002 : `sentients create module`
│   │
│   ├── Epic E-002 : Authentification
│   │   ├── Story S-003 : `sentients connect` (email/password)
│   │   ├── Story S-004 : `sentients connect` (MFA TOTP)
│   │   └── Story S-005 : `sentients disconnect`
│   │
│   └── Epic E-003 : Build & Informations
│       ├── Story S-006 : `sentients pack`
│       └── Story S-007 : `sentients -v` + `sentients help`
│
├── Release 0.2.0 (Store)
│   │
│   ├── Epic E-004 : Publication
│   │   ├── Story S-008 : `sentients publish`
│   │   ├── Story S-009 : `sentients link`
│   │   └── Story S-010 : `sentients unlink`
│   │
│   ├── Epic E-005 : Validation
│   │   ├── Story S-011 : `sentients audit`
│   │   └── Story S-012 : `sentients debug`
│   │
│   └── Epic E-008 : Signature numérique
│       ├── Story S-019 : `sentients sign keygen` (génération clés Ed25519)
│       ├── Story S-020 : `sentients sign <module>` (signature archive .SenMod)
│       └── Story S-021 : `sentients sign verify <module>` (vérification signature)
│
└── Release 0.3.0 (Qualité)
    │
    ├── Epic E-006 : Expérience développeur
    │   ├── Story S-013 : Mode verbose / logs
    │   ├── Story S-014 : Configuration `sentients.config.json`
    │   └── Story S-015 : Auto-update detection
    │
    └── Epic E-007 : Tests & CI
        ├── Story S-016 : Suite de tests unitaires
        ├── Story S-017 : Tests E2E (testscript)
        └── Story S-018 : Pipeline CI/CD (GoReleaser)
```

---

## 14. Risques

| ID | Risque | Probabilité | Impact | Mitigation |
|----|--------|-------------|--------|------------|
| R-001 | API `sentient-connect` non disponible | Moyenne | Élevé | Mode offline pour les commandes locales (init, create, pack, audit, debug) |
| R-002 | Incompatibilité keychain sur certaines distributions Linux | Moyenne | Moyen | Fallback transparent fichier chiffré AES-256-GCM (`credentials.enc` / `signing.enc`), clé PBKDF2 du secret machine, `SENTIENT_CLI_STORE` pour forcer le backend |
| R-003 | Taille du binaire trop élevée | Faible | Faible | `ldflags -s -w`, UPX compression optionnelle |
| R-004 | Breaking changes API `sentient-connect` | Faible | Élevé | Versioning API, détection automatique de la version |
| R-005 | Conflits de noms de modules | Moyenne | Moyen | Validation stricte, vérification d'unicité avant création |
| R-006 | Archive `.SenMod` corrompue ou falsifiée | Faible | Élevé | Signature numérique Ed25519 (`sentients sign`), vérification avant publication |
| R-007 | MFA bloquant (appareil perdu) | Faible | Élevé | Backup codes, procédure de récupération via `sentient-connect` web |

---

## 15. ADR (Architecture Decision Records)

### ADR-001 : Go + Bubbletea comme stack technique

**Contexte** : Choisir la stack technique pour la CLI Sentient.

**Options considérées** :
- **Option A** : Go + Cobra + Bubbletea
- **Option B** : Node.js + Commander + Ink
- **Option C** : Rust + Clap + Ratatui
- **Option D** : Python + Click + Rich

**Décision** : Option A — Go + Cobra + Bubbletea

**Justification** :
- Go produit des binaires statiques sans dépendance runtime
- Cobra est le standard Go pour les CLI (utilisé par Hugo, Kubernetes, Docker)
- Bubbletea offre une TUI riche et réactive (framework mature, bonne documentation)
- Écosystème Charm (Lipgloss, Bubbles) très complet
- Compilation croisée simple (Linux, macOS, Windows)
- Performance : démarrage < 100ms

**Conséquences** :
- Pas de JS/TS partagé avec le frontend (mais la CLI est un outil indépendant)
- Apprentissage de Bubbletea pour l'équipe
- Binaire plus gros que Python/Node mais sans dépendance

### ADR-002 : Keychain natif pour les credentials

**Contexte** : Stocker les tokens d'authentification de manière sécurisée.

**Options considérées** :
- **Option A** : Keychain système (go-keyring)
- **Option B** : Fichier chiffré (AES-256-GCM)
- **Option C** : Variable d'environnement

**Décision** : Option A — Keychain natif

**Justification** :
- Sécurité maximale : le OS gère le chiffrement et l'accès
- Pas de mot de passe maître à gérer
- Fallback fichier chiffré pour les environnements sans keychain

**Conséquences** :
- Dépendance au keychain système (fallback nécessaire)
- Sur Linux, nécessite un agent secret (gnome-keyring, KWallet)
