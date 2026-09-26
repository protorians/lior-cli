# Liora CLI (`liora`)

> **Statut : IMPLÉMENTÉ (binaire `liora`, spec alignée sur le code)**
>
> Ce document est la **spécification SpecKit de la CLI `liora`**, outil en ligne de commande
> permettant aux développeurs d'initialiser, créer, construire, auditer, déboguer et publier des
> modules Liora via un compte développeur `liorian-connect`.
>
> - **Stack technique** : Go (1.26, Cobra) + Bubbletea (TUI lipgloss/charmbracelet)
> - **Distribution** : binaire unique multi-plateforme (Linux, macOS, Windows)
> - **État du code** : implémenté dans `protorians/lior-cli` (branche `alpha`) ; dernière release documentée 0.20.0 ;
>   les écarts constatés entre la spec et le code sont documentés dans `docs/rapport-implementation.md`
>
> **Specs satellite** : les commandes outillage `dev`/`build`/`start`/`check` (passe-plat vers les
> scripts `package.json` du projet, section `toolchain` de `lorian.config.json`) sont spécifiées
> dans `docs/specs/liora-toolchain.md`.
>
> **Documents de référence (workspace `liorian-workspace/docs`)** : la présente spec s'aligne sur
> les contrats du workspace, en particulier le manifest de module
> (`docs/modules/module-manifest.md`, schéma JSON `@liorian/sdk/schemas/module.schema.json`)
> et la distribution des responsabilités Connect / Store / Core
> (`docs/specs/applications/module-distribution.md`, `docs/modules/store.md`, `docs/specs/applications/liorian-connect.md`).
>
> Chaîne d'installation des modules tiers (`docs/specs/applications/module-installation.md`,
> `docs/specs/modules/module-installation/module.spec.md`, statut `EXÉCUTÉ` 0.49.0–0.50.0) :
> la CLI en est le premier maillon côté éditeur — artefact `.liozip` (ADR-003), signature Ed25519
> obligatoire de la charge utile canonique (ADR-010, §7.1), manifeste canonique (`compatibility`,
> `oauth`, `capabilities`, permissions `Role:Verbe`), types d'exécution (`CONFIGURATION`,
> `WEB_APP_LOCAL`, … — ADR-004/ADR-011) et vérification fail-closed (SEC-001, §7.2).

---

## Métadonnées SpecKit

| Propriété | Valeur |
|-----------|--------|
| Identifiant | `liora` |
| Nom | Liora CLI |
| Rôle | Outil CLI pour le cycle de vie complet des modules Liora |
| Type de spécification | Application Spec |
| Version de spécification | `0.1.0` (candidate) |
| Statut de la version | `active` (spec) — implémentée (rel. 0.20.0) |
| Langue | Document en français ; interface bilingue fr-FR / en-US (i18n §11.2) |
| Emplacement cible (SpecKit) | `liora.md` |

---

## 1. Vision Produit

### 1.1 Objectif

Liora CLI est l'outil de développement unique pour tout développeur souhaitant créer, maintenir
et publier des modules dans l'écosystème Liora. Elle couvre le cycle de vie complet :

```
init → create → develop → debug → audit → pack → sign → link → publish
                ↑                                               │
                └───────────────────────────────────────────────┘
```

### 1.2 Public cible

| Acteur | Usage |
|--------|-------|
| Développeur Liora | Initialiser un projet, créer/modifier des modules, publier sur le store |
| Équipe interne | Audit automatique, validation des conventions, debug |

### 1.3 Valeur ajoutée

- **Zéro configuration manuelle** : détection automatique des outils disponibles sur la machine
- **Sécurité native** : credentials chiffrés, MFA supportée, token rotation
- **Validation continue** : audit des règles Clean Architecture + conformité manifest
- **Intégration Liora Connect** : publication one-shot vers le store

---

## 2. Portée

### Dans le périmètre (In Scope)

- `liora init` — Initialisation d'un projet Liora (téléchargement de la release template + deps + `.env`)
- `liora create module` — Création de module dans `library/modules/`
- `liora create view` — Création d'une vue de présentation dans un module existant
- `liora connect` — Authentification développeur (credentials + MFA)
- `liora auth` — Authentification OAuth2 (code d'autorisation + PKCE, navigation navigateur)
- `liora disconnect` — Suppression des credentials
- `liora pack` — Build + compression d'un module (`.liozip`)
- `liora sign` — Signature numérique Ed25519 des archives `.liozip` (keygen / sign / verify)
- `liora publish` — Publication dans le store via Liora Connect
- `liora link` — Liaison module local ↔ module en ligne
- `liora unlink` — Dé liaison module local ↔ module en ligne
- `liora debug <module>` — Debug d'un ou tous les modules
- `liora test <module>` — Exécution des tests d'un ou tous les modules
- `liora audit <module>` — Audit de conformité d'un ou tous les modules
- `liora repair [module]` — Réparation automatique des anomalies bloquantes d'un ou tous les modules
- `liora dev|build|start|check` — Outillage applicatif (spécifié dans `docs/specs/liora-toolchain.md`)
- `liora help` — Affichage de l'aide
- `liora -v | --version` — Affichage de la version

### Hors périmètre (Out of Scope)

- Gestion du contenu des modules (pages, composants, API)
- Monitoring temps réel des modules en production
- Gestion des organisations / équipes
- CI/CD intégré (workflow GitHub Actions séparé)

### Périmètre futur (Future Scope)

- Catalogue de modules tiers distribué (le storefront `liora marketplace` est disponible en lecture/installation)

---

## 3. Exigences

### Exigences fonctionnelles

| ID | Description |
|----|-------------|
| FR-001 | La CLI détecte automatiquement les gestionnaires de paquets disponibles (bun, pnpm, yarn, npm) et propose le choix à l'utilisateur |
| FR-002 | `liora init` télécharge la release (ZIP) du template `protorians/liorian-socle` dans le répertoire courant, selon un canal (`stable` par défaut, `alpha`, `beta`, `rc`) |
| FR-003 | `liora init` installe les dépendances avec le gestionnaire choisi |
| FR-004 | `liora create module` crée un module dans `library/modules/<domain>/` (domaine reverse-DNS + identifiant kebab-case) à partir d'un mockup de référence embarqué (Clean Architecture, structure standardisée) |
| FR-005 | `liora create module` génère un token UUID unique dans `manifest.json` et un manifeste conforme au schéma du workspace (`module-manifest.schema.json`, `schemaVersion: 1`) |
| FR-006 | `liora connect` authentifie le développeur via `liorian-connect` (email + mot de passe) |
| FR-007 | `liora connect` supporte le MFA (TOTP, backup codes) |
| FR-008 | `liora connect` stocke les credentials de manière sécurisée (keychain/credential store, fallback vault chiffré) |
| FR-009 | `liora disconnect` supprime toutes les credentials stockées |
| FR-010 | `liora pack` compresse `library/modules/<module>/` + `public/assets/<module>/` + `src/app/<module.uri>/` en `.liozip` (manifeste canonique à la racine) |
| FR-011 | `liora pack` déplace l'archive vers `.lorian/build/` (`--out` pour un chemin personnalisé, `--version` pour forcer la version) |
| FR-012 | `liora publish` construit, audite puis publie via l'API developer-store (produit → version → artefact) |
| FR-013 | `liora publish` demande les métadonnées du module si non définies |
| FR-014 | `liora link` lie un module local à un module distant (token produit, mode CI `link <module> <token>`) et persiste l'état dans `.lorian/links.json` |
| FR-015 | `liora unlink` délie un module local de `liorian-connect` (option `--sync-remote` pour synchroniser les métadonnées locales) |
| FR-016 | `liora debug` lance le debug d'un module ou de tous les modules |
| FR-017 | `liora audit` vérifie la conformité Clean Architecture, `manifest.json` et `index.tsx` |
| FR-018 | `liora audit` vérifie que les `requirements` existent et que les `dependencies` du `package.json` sont installées |
| FR-019 | `liora help` affiche l'aide contextuelle des commandes |
| FR-020 | `liora -v` / `liora --version` affiche la version actuelle |
| FR-021 | `liora sign keygen` génère une paire de clés Ed25519 et la stocke dans le keychain système |
| FR-022 | `liora sign <module\|archive>` signe la charge utile canonique de publication du module et produit un fichier `.sig` |
| FR-023 | `liora sign verify <module>` vérifie la validité de la signature `.sig` d'un module |
| FR-024 | `liora sign` affiche le fingerprint SHA-256 de la clé publique du développeur |
| FR-025 | La langue de l'interface est résolue dans l'ordre : `--lang` → `LIORIAN_CLI_LANG` → `cli.lang` de `lorian.config.json` → locale OS (LC_ALL/LC_MESSAGES/LANG), avec repli sur `en-US` |
| FR-026 | `liora auth` authentifie le développeur via le flux OAuth2 **code d'autorisation + PKCE** (navigateur + serveur local en boucle), complément du `liora connect` (email/mot de passe) |
| FR-027 | `liora auth` stocke la session OAuth (`access_token`, `refresh_token`, expiration) dans le keychain (repli vault chiffré) et la partage avec les commandes authentifiées |
| FR-028 | `liora init` génère le `.env` depuis le fichier d'exemple du template (`.env-sample` & co) : clé applicative, nom/slug du projet et paire VAPID synthétisés, variables restantes proposées avec leur valeur suggérée ; un `.env` existant n'est jamais écrasé |
| FR-029 | `liora init` accepte `--auto-env` (ou `LIORIAN_CLI_ENV_AUTO`) pour ne poser aucune question (nom du projet, gestionnaire de paquets, `.env`) et accepter les valeurs suggérées ; sans cette option, une confirmation globale est proposée puis un prompt par variable (Échap = accepter le reste) |
| FR-030 | `liora create view <module> [name]` génère une vue de présentation `presentation/views/<name>.view.tsx` depuis le mockup embarqué (composant, titre, description renommés) |
| FR-031 | `liora repair [module]` répare automatiquement les anomalies bloquantes d'un audit (champs de manifeste, nom du dossier, dépendances npm manquantes, JSON malformé), re-audite puis liste en instructions les points non réparables |
| FR-032 | `liora dev`, `build`, `start` et `check` proxient les scripts `package.json` du projet et exécutent l'`audit` de conformité des modules en pre-flight (`dev`/`build`/`start`) — spécifié dans `docs/specs/liora-toolchain.md` (TFC-001 → TFC-017) |

### Exigences non-fonctionnelles

| ID | Description |
|----|-------------|
| NFR-001 | Binaire unique, sans dépendance externe (static linking) |
| NFR-002 | Temps de démarrage < 100ms |
| NFR-003 | Compatible Linux (amd64, arm64), macOS (amd64, arm64), Windows (amd64) |
| NFR-004 | Sortie terminal compatible UTF-8 + 256 couleurs minimum |
| NFR-005 | Logs activables via `--verbose` ou variable d'environnement `LIORIAN_CLI_DEBUG` |
| NFR-006 | Mises à jour auto-detectées (notification, pas de mise à jour forcée) |
| NFR-007 | Interface bilingue `en-US` (défaut) / `fr-FR` via catalogues i18n embarqués dans le binaire, avec repli sur `en-US` |

### Exigences de sécurité

| ID | Description |
|----|-------------|
| SEC-001 | Credentials stockés dans le keychain système (macOS Keychain, Linux secret-service, Windows Credential Manager) |
| SEC-002 | Jamais de credentials CLI (tokens, mots de passe) en clair sur disque (keychain ou vault chiffré uniquement) ; le `.env` généré par `init` ne contient que des secrets **applicatifs** (clé de chiffrement, paire VAPID), jamais de credentials de développeur |
| SEC-003 | Tokens d'accès avec durée de vie limitée, rotation automatique |
| SEC-004 | Chiffrement des données sensibles au repos (AES-256-GCM pour les caches) |
| SEC-005 | Validation stricte des inputs (UUID, noms de module, URLs) |
| SEC-006 | Mode MFA obligatoire si activé sur le compte développeur |
| SEC-007 | Les archives `.liozip` ne contiennent jamais de credentials ou tokens |
| SEC-008 | Les clés de signature Ed25519 sont stockées dans le keychain OS, jamais en clair sur disque |
| SEC-009 | La signature numérique Ed25519 de la charge utile canonique garantit l'intégrité et l'authenticité des archives `.liozip` avant publication (obligatoire, ADR-010) |

### Exigences techniques

| ID | Description |
|----|-------------|
| TECH-001 | Go 1.22+ comme langage de développement (go.mod : 1.26) |
| TECH-002 | Bubbletea comme framework TUI pour les interactions utilisateur |
| TECH-003 | Lipgloss pour le styling terminal |
| TECH-004 | Bubbles pour les composants TUI réutilisables — priorité stricte aux composants natifs bubbles avant tout composant custom (voir §9.1) |
| TECH-005 | GoReleaser pour la compilation multi-plateforme et le packaging |
| TECH-006 | Architecture en couches : commands → services → infrastructure |
| TECH-007 | Configuration via fichier `lorian.config.json` optionnel dans le projet |
| TECH-008 | Communication avec `liorian-connect` via REST API HTTPS |
| TECH-009 | Registre d'applications `app.config.json` embarqué dans le binaire (`baseUrl`/`timeout` par application API), surchargeable par un `app.config.json` local et `LIORIAN_AUTH_API` |

---

## 4. Architecture

### 4.1 Architecture en couches

```
lior-cli/
├── main.go                        # Point d'entrée (variables version/commit/date + //go:embed app.config.json)
├── cmd/                           # Commandes CLI (couche présentation, Cobra)
│   ├── root.go                    # Commande racine (flags --verbose, --no-color, --lang, update check)
│   ├── init.go                    # liora init (--channel alpha|beta|rc|stable, --auto-env)
│   ├── init_env.go                # Génération du .env depuis l'exemple du template (FR-028/-029)
│   ├── create.go                  # liora create module
│   ├── create_view.go             # liora create view (FR-030)
│   ├── connect.go                 # liora connect
│   ├── auth.go                    # liora auth (OAuth2 code + PKCE)
│   ├── disconnect.go              # liora disconnect
│   ├── pack.go                    # liora pack
│   ├── sign.go                    # liora sign (keygen / sign / verify)
│   ├── publish.go                 # liora publish
│   ├── link.go                    # liora link + unlink (--sync-remote)
│   ├── marketplace.go             # liora marketplace search|install
│   ├── debug.go                   # liora debug
│   ├── test.go                    # liora test
│   ├── audit.go                   # liora audit (--output table|json)
│   ├── repair.go                  # liora repair (--dry-run, --warnings, --no-install, …)
│   ├── toolchain.go               # liora dev|build|start|check
│   ├── gate.go                    # Porte de santé des modules (pre-flight audit)
│   ├── modules.go                 # Helpers de résolution projet/module (code 3)
│   └── localize.go                # Helpers i18n (MessageKey, résolution langue)
├── internal/
│   ├── config/                    # Configuration projet & CLI
│   │   ├── config.go              # Lecture/écriture lorian.config.json
│   │   └── paths.go               # Résolution des chemins projet
│   ├── appconfig/                 # Registre d'applications embarqué (app.config.json, TECH-009)
│   │   └── appconfig.go           # BaseURL/timeout par API, surcharge locale/env
│   ├── i18n/                      # Internationalisation (NFR-007)
│   │   ├── i18n.go                # Résolution langue, lookup de clés, fallback en-US
│   │   └── locales/               # Catalogues embarqués en-US.json, fr-FR.json
│   ├── auth/                      # Authentification & credentials
│   │   ├── credentials.go         # Keychain + fallback vault chiffré (LIORIAN_CLI_STORE)
│   │   ├── connector.go           # Client API liorian-connect
│   │   ├── mfa.go                 # Logique MFA (TOTP, backup codes)
│   │   ├── oauth.go               # Flux OAuth2 authorization_code + PKCE (navigateur, callback loopback)
│   │   └── session.go             # Session locale (token cache, refresh)
│   ├── module/                    # Logique module
│   │   ├── creator.go             # Création de module
│   │   ├── view.go                # Création d'une vue de présentation (ViewCreator)
│   │   ├── scaffold.go            # Scaffolding depuis le mockup embarqué (renommage arborescence)
│   │   ├── mockups/               # hello-world/ + page.tsx + view.tsx (mockups embarqués)
│   │   ├── manifest.go            # Manipulation manifest.json
│   │   ├── packer.go              # Compression .liozip (limite 50 MB, quotas, manifest racine)
│   │   ├── linker.go              # Liaison local ↔ distant + état .lorian/links.json
│   │   ├── validator.go           # Validation module
│   │   └── module_test.go         # Tests unitaires
│   ├── repair/                    # Réparation automatique des anomalies d'audit (FR-031)
│   │   └── repair.go              # Repairer (dry-run, warnings, no-install, rename)
│   ├── toolchain/                 # Outillage dev/build/start/check (docs/specs/liora-toolchain.md)
│   │   └── toolchain.go           # Résolution de script, hooks, exécution passthrough
│   ├── moduletest/                # Exécution des tests de module (liora test)
│   │   ├── testrunner.go          # Détection runner + streaming des logs
│   │   └── catalog.go             # Catalogue de packages de test
│   ├── catalog/                   # Catalogue public (liora marketplace)
│   │   ├── catalog.go             # Recherche dans le storefront
│   │   └── install.go             # Installation vérifiée/signée d'un module
│   ├── runner/                    # Exécution de processus (groupes POSIX/Windows)
│   │   ├── runner.go              # Lancement/streaming des commandes
│   │   ├── proc_unix.go           # Groupe de process POSIX (arrêt de l'arbre)
│   │   └── proc_windows.go        # Arrêt de process Windows
│   ├── signing/                   # Signature numérique Ed25519
│   │   ├── signer.go              # Génération clés, signature, vérification
│   │   └── keystore.go            # Stockage clés (keychain + fallback chiffré signing.enc)
│   ├── audit/                     # Audit de conformité
│   │   └── auditor.go             # Orchestrateur d'audit (règles manifest/bootstrap/deps)
│   ├── debug/                     # Debug de module
│   │   └── debugger.go            # Étapes de debug, résolution du build, streaming, timeout
│   ├── store/                     # Publication store
│   │   ├── builder.go             # Construction archive
│   │   └── publisher.go           # Publication via API developer-store (produit → version → artefact)
│   ├── tui/                       # Composants Bubbletea
│   │   ├── components.go          # SummaryCard, Wordmark, StepsList, LogsBlock (lipgloss)
│   │   ├── styles.go              # Palette brand sage/olive + thème dark/light
│   │   ├── prompts.go             # AskText/AskTextAuto, Confirm, Select (degradation non-interactive)
│   │   ├── spinner.go             # RunWithSpinner (indicateur de progression)
│   │   ├── progress.go            # RunWithProgress/RunWithProgressDetail (barre + ligne de détail)
│   │   ├── report.go              # ReportHeading, StatusChip, CountsLine (rapports audit/repair)
│   │   ├── step.go                # RunWithSteps + vocabulaire d'étapes (statuts, résumé)
│   │   └── table.go               # Tableau arrondi custom (lipgloss)
│   └── pkg/                       # Utilitaires
│       ├── errors.go              # Erreurs catégorisées + codes de sortie §11.1
│       ├── fs.go                  # Opérations fichiers
│       ├── git.go                 # Exécution de commandes externes
│       ├── github.go              # ResolveRelease/FetchReleaseZip (release init)
│       ├── http.go                # Client HTTP + enveloppe Raiton + APIError
│       ├── env.go                 # Parseur dotenv + génération (clé, VAPID) pour init
│       ├── nodepackage.go         # Lecture package.json + dépendances latest
│       ├── pm.go                  # Gestionnaires de paquets (détection, args, installation)
│       ├── semver.go              # Comparaison/incrément de versions
│       ├── uuid.go                # Génération UUID
│       ├── crypto.go              # MachineSecret (PBKDF2), EncryptVault/DecryptVault (AES-256-GCM)
│       ├── open.go                # Ouverture du navigateur (open / rundll32 / xdg-open)
│       └── update.go              # Détection de mises à jour (NFR-006, cache 24 h)
├── e2e/                           # Tests E2E
│   ├── e2e_test.go                # Générateur testscript (TC-001 → TC-040 vs mock API)
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
| `archive/zip` | Compression .liozip (stdlib) | — |

> Le parsing TOML (`BurntSushi/toml`) a été retiré : la configuration est uniquement JSON
> (`lorian.config.json`). `lorian.config.toml` ne sert plus que de marqueur de projet.

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

### 5.1 `liora init`

#### Purpose

Initialiser un nouveau projet Liora en téléchargeant la release (ZIP) du template
`protorians/liorian-socle` et en installant les dépendances. La source est `--channel`
("stable" par défaut) ; `LIORIAN_CLI_TEMPLATE_REPO` peut la remplacer par une URL GitHub,
une URL ZIP directe ou un répertoire local (tests/miroirs). Le `.env` du projet est généré
depuis l'exemple livré par le template.

#### Comportement

1. **Déterminer le nom du projet** : argument positionnel optionnel, sinon input Bubbletea
   (défaut : nom du dossier courant). Avec `--auto-env` (ou `LIORIAN_CLI_ENV_AUTO`), le nom est
   déduit du dossier courant sans question.
2. **Résoudre le canal de release** (`--channel alpha|beta|rc|stable`, défaut `stable`) :
   la release la plus récente du canal est téléchargée en ZIP via l'API GitHub ; la ligne de
   téléchargement affiche la version (tag), le canal, la branche cible et le commit de la release
   (branche/commit best-effort, `inconnu` si indisponibles)
3. **Gérer la destination existante** : dossier non vide → proposer *Annuler* / *Fusionner* /
   *Vider* (cwd) / *Supprimer* ; jamais effacé sans approbation
4. **Détection automatique** des gestionnaires de paquets disponibles sur la machine :
   - `bun` → disponible ? (recommandé, premier du fil)
   - `pnpm` → disponible ?
   - `yarn` → disponible ?
   - `npm` → disponible ?
5. **Proposer le choix** via un sélecteur Bubbletea (liste filtrée aux disponibles) ; ignoré en
   mode `--auto-env` (le premier disponible, bun recommandé, est retenu)
6. **Télécharger la release** avec barre de progression `RunWithProgress` (extraction dans la
   destination ; en cas de fusion, téléchargement dans un dossier temporaire puis copie)
7. **Installer les dépendances** avec le gestionnaire sélectionné (échec → warning non bloquant) ;
   les dépendances explicitement pinnées `latest` sont réinstallées (`<pm> add <pkg>@latest`)
8. **Écrire `lorian.config.json`** (racine `project.name` + `project.packageManager`)
9. **Générer le `.env`** depuis l'exemple du template (FR-028) :
   - un `.env` existant n'est **jamais écrasé** ; sans exemple, l'étape est ignorée
   - les variables connues sont synthétisées : clé applicative (`APP_KEY` / `*ENCRYPTION_KEY`),
     nom et slug (`*APP_NAME`, `*APP_SLUG`), paire VAPID (`*VAPID_PUBLIC_KEY` + clé privée)
   - les autres variables sont proposées avec leur valeur suggérée en placeholder (Tab pour
     remplir) ; `Échap` conserve la valeur courante et accepte le reste des suggestions
   - `--auto-env` / `LIORIAN_CLI_ENV_AUTO` accepte toutes les suggestions sans question ; sans
     cela, une confirmation globale est d'abord proposée, puis un prompt par variable
   - fichier écrit en `0600`, commentaires/ordre/guillemets de l'exemple préservés
10. **Afficher le résumé** : projet initialisé, gestionnaire utilisé, `.env` généré, prochaines étapes

#### Flags

| Flag | Description |
|------|-------------|
| `--channel string` | canal de release (`alpha`, `beta`, `rc`, `stable` ; défaut `stable`) |
| `--auto-env` | auto-configurer tout avec les valeurs par défaut : ignorer les questions du nom du projet, du gestionnaire de paquets et du `.env` |

#### Contraintes

- Si aucun gestionnaire n'est détecté → erreur explicite avec instructions d'installation
- `--channel` doit être l'un des canaux valides (erreur listant les choix sinon)
- Pas de clone git : téléchargement de l'archive de release (rapide, sans historique git)
- En mode non-interactif, un dossier existant est vidé uniquement si `LIORIAN_CLI_YES` est défini, sinon refus explicite
- Le `.env` généré n'est jamais un doublon : un fichier existant est conservé tel quel
- Les invites du `.env` s'exécutent **hors** du spinner (une invite Bubbletea ne peut pas posséder le terminal pendant le spinner)

#### Sortie TUI

```
Destination : /chemin/vers/mon-projet
? Nom du projet : mon-projet
? Gestionnaire de paquets : bun (recommandé)
  ⠋ Téléchargement de la release de liorian-socle
  [================--------------------] 45%
  release v0.20.0 · canal stable · branche alpha · commit e7cbd2f
  ⠋ Installation des dépendances...
  ⠋ Configuration de l'environnement
? Auto-configurer le .env avec les valeurs suggérées ? (Y/n)
? Valeur pour App Name : mon-projet [tab: fill] [esc: auto-fill the rest]

  ✓ Projet initialisé avec succès
    Gestionnaire de paquets : bun
    Environnement : .env

  Prochaines étapes :
    cd mon-projet
    liora connect
    liora create module
```

---

### 5.2 `liora create module`

#### Purpose

Créer un nouveau module dans `library/modules/<domain>/` à partir d'un **mockup de référence
embarqué** dans le binaire (Clean Architecture, structure standardisée) — FR-004. Aucun checkout
externe requis. Le manifeste produit respecte le contrat canonique du workspace
(`schemaVersion: 1`, `docs/modules/module-manifest.md`).

#### Comportement

1. **Vérifier le contexte** : être à la racine d'un projet Liora (`lorian.config.json`,
   `lorian.config.toml` ou présence de `library/modules/`)
2. **Demander l'identité** du module (chaque prompt a un flag équivalent) :
   - **domaine** reverse-DNS (`--domain`, ex. `com.organization.domain`) — validation `ValidateDomain`,
     il nomme le dossier `library/modules/<domain>/` et le champ `manifest.domain`
   - **identifiant** kebab-case (`--id`, ou argument positionnel `create module <name>`) — 3-64,
     validation `ValidateName`, il nomme les composants et le champ `manifest.id`
   - **nom applicatif** affiché (`--name`, défaut : Title Case de l'identifiant)
   - **version** SemVer optionnelle (`--version`, défaut `0.0.0`)
   - **icône** lucide en PascalCase (`--icon`, défaut `PuzzleIcon`)
   - **url** de page (`--url`, défaut : identifiant) → `manifest.uri = /<url>` et
     `src/app/<url>/page.tsx`
   - **description** (`--description`)
   - **type** de distribution (`--type`, enum canonique `ModuleType`, défaut `WEB_APP_LOCAL` ; `CONFIGURATION` recommandé pour les modules déclaratifs sans code — ADR-011, §7.5 de la spec installation)
   - **catégorie** de store (`--category`, enum `ModuleCategory`, défaut `SYSTEM`)
3. **Résoudre la source du mockup module** (ordre de priorité) :
   - `--mockup` (champ `Creator.MockupDir`)
   - `LIORIAN_MODULE_MOCKUP` (répertoire de module de référence)
   - mockup **embarqué** `internal/module/mockups/hello-world/`
   (une source custom doit ressembler à un module scaffoldable : `manifest.json` + `index.tsx`)
4. **Scaffolder le module** (copie + renommage, `internal/module/scaffold.go`) :
   - Les fichiers et identifiants du mockup sont renommés selon les 6 variantes de
     l'**identifiant** (`Hello World` → affichable, `HelloWorld` → PascalCase,
     `helloWorld` → camelCase, `hello-world` → kebab-case, `HELLO_WORLD` → UPPER_SNAKE,
     `helloworld` → minuscules)
   - Le contenu des fichiers textes est réécrit en conséquence (renommage des fichiers inclus)
5. **Vérifier les requirements** : chaque module du champ `requirements` du `manifest.json` doit
   exister localement — dans `library/modules/` **ou** dans `src/modules/` (modules internes
   de la plate-forme). Les modules cœur de la plate-forme `organization` et `identity` sont
   toujours considérés satisfaits. En cas de module requis manquant, la création **échoue**,
   le répertoire scaffoldé ainsi que la page éventuelle sont **supprimés** (rollback) et une
   erreur catégorisée listant les modules manquants est renvoyée
   (`create.error.requirements_missing` + `create.error.requirements_missing.fix`)
6. **Installer les dépendances** : les `dependencies` et `devDependencies` du `package.json` du
   module (le manifeste ne les porte plus) sont résolues via le gestionnaire de paquets détecté
   (`bun` → `pnpm` → `yarn` → `npm`, voir `config.DetectPackageManager`) exécuté à la racine du
   projet. Les dépendances explicitement versionnées `latest` sont ensuite réinstallées
   explicitement (`<pm> add <pkg>@latest`) car un simple `install` les ignore souvent. Si aucun
   gestionnaire n'est détecté, un avertissement est affiché (les dépendances ne sont pas
   installées). Un échec d'installation est non-bloquant (simple avertissement
   `create.warn.install`). L'installation peut être désactivée avec le drapeau `--skip-install`
   (ou l'environnement `LIORIAN_CLI_SKIP_INSTALL=1`).
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
   `--page-mockup` (champ `Creator.PageMockup`) → `LIORIAN_PAGE_MOCKUP` → page **embarquée**
   `internal/module/mockups/page.tsx`
9. **Afficher le résumé** : module créé, token généré, page créée (si uri), prochaines étapes

#### Structure générée (mockup embarqué hello-world)

```
library/modules/<domain>/
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
> `View.Status`, et `Activity.Container` / `Activity.Loader` (importés depuis `@liorian/sdk`).

`manifest.json` — le scaffold part du manifeste du mockup embarqué (miroir du module `hello-world`
du socle `liorian-socle`, conforme au contrat canonique du workspace) et réécrit `id`, `domain`,
`key`, `name`, `description`, `version`, `icon`, `uri` :

```json
{
  "$schema": "../../node_modules/@liorian/sdk/schemas/module.schema.json",
  "schemaVersion": 1,
  "id": "<id>",
  "domain": "<domain>",
  "key": "<ID_UPPER_SNAKE>",
  "name": "<Nom affiché>",
  "description": "<description>",
  "version": "<SemVer>",
  "icon": "<IconName>",
  "type": "<ModuleType : CONFIGURATION par défaut recommandé pour le déclaratif, WEB_APP_LOCAL pour le code scaffoldé>",
  "external": true,
  "entry": "index.tsx",
  "uri": "/<url>",
  "category": "<Catégorie>",
  "token": "<UUID v4 généré>",
  "publisher": { "id": "", "name": "" },
  "platforms": {
    "web": { "supported": true, "modes": ["web"] },
    "desktop": { "supported": true, "modes": ["local-webview"], "os": ["windows", "macos", "linux"] },
    "mobile": { "supported": true, "modes": ["local-webview"], "os": ["android"] }
  },
  "compatibility": {
    "socle": { "min": "0.17.1", "max": "0.17.x" },
    "api": { "min": "0.27.0", "max": "0.27.x" }
  },
  "permissions": ["User:Get", "Editor:Post", "Editor:Put", "Admin:Delete"],
  "oauth": { "scopes": ["openid", "profile", "email", "organizations"] },
  "capabilities": ["core:default"],
  "isEnabled": true,
  "isDefault": false,
  "requirements": { "organization": ">=1.0.0", "identity": ">=1.0.0" },
  "optionalRequirements": {},
  "widgets": ["analytics"],
  "routines": ["<camelId>AnalyticsRoutine"],
  "providers": ["layout"],
  "menu": { "items": [{ "label": "<Nom affiché>", "icon": "<IconName>", "url": "/<url>" }] }
}
```

> **Contrat canonique** : la référence des champs (identité, plateformes, compatibilité,
> permissions, capacités, activation, prérequis, interface) et les conventions de nommage sont
> dans `docs/modules/module-manifest.md`. Le schéma JSON (`module-manifest.schema.json`,
> `schemaVersion: 1`) est publié avec le SDK (`@liorian/sdk`) et sert de référence à la validation
> CI. Les champs `requirements` et `optionalRequirements` sont **strictement** gérés par le
> manifeste (jamais dupliqués dans `index.tsx`) ; les dépendances npm (`dependencies` /
> `devDependencies`) sont portées par le `package.json` du module.

`index.tsx` (déclaration déclarative, pas de `render` asynchrone) :

```tsx
import {ModuleDeclarationInterface} from "@liorian/sdk/domain/entities/module.interface";

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

> **Séparation manifeste / déclaration** : `requirements` et `optionalRequirements` sont
> **uniquement** déclarés dans `manifest.json` (voir `docs/modules/module-manifest.md` § 3) — ils
> ne figurent plus dans `index.tsx`. Les dépendances npm sont déclarées dans le `package.json` du
> module. Le manifeste est la source de vérité ; la déclaration React en est la projection
> runtime.

`src/app/<url>/page.tsx` (scaffoldé quand la déclaration porte un `uri`/`url`) :

```tsx
import {<PascalId>View} from "@/library/modules/<domain>/presentation/views/<id>.view";

export default function <PascalId>Page() {
    return <<PascalId>View/>;
}
```

#### Contraintes

- Le token UUID est **unique** et généré à la création (injection dans le manifest scaffoldé)
- Le **domaine** (reverse-DNS) ne peut pas entrer en conflit avec un module existant
  (`library/modules/<domain>/`) ; l'identifiant doit être kebab-case (3-64)
- Le renommage est complet : fichiers **et** identifiants (imports, `identifier`, `key`, `uri`)
- Le manifeste est conforme au schéma canonique (24 champs requis, `schemaVersion: 1`) ;
  `entry` pointe vers `index.tsx` et `domain` correspond au dossier du module
- Les champs `requirements` / `optionalRequirements` ne sont présents que dans `manifest.json`
  (jamais dans `index.tsx`) ; les dépendances npm vivent dans le `package.json` du module
- `--mockup` / `--page-mockup` (et `LIORIAN_MODULE_MOCKUP` / `LIORIAN_PAGE_MOCKUP`) permettent
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

  ✓ Module créé : library/modules/com.example.blog-manager/
  ✓ Token généré : a1b2c3d4-e5f6-7890-abcd-ef1234567890
  ✓ manifest.json initialisé
  ✓ index.tsx initialisé
  ✓ Page : src/app/blog-manager/page.tsx

  Prochaines étapes :
    liora connect
    liora pack com.example.blog-manager
    liora publish
```

---

### 5.3 `liora connect`

#### Purpose

Authentifier le développeur avec son compte `liorian-connect` et stocker les credentials de
manière sécurisée.

#### Comportement

1. **Vérifier si déjà connecté** : credentials existantes dans le keychain
   - Si oui → afficher le statut et demander si reconnexion souhaitée
2. **Demander l'email** via input Bubbletea
3. **Demander le mot de passe** via input Bubbletea (masqué)
4. **Envoyer les credentials** à l'API `liorian-connect` (`POST /api/auth/sign-in`)
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
| `access_token` | Keychain (`lorian-cli.access_token`) | Oui (keychain natif) |
| `mfa_token` | Keychain (`lorian-cli.mfa_token`) | Oui (keychain natif) |
| `device` | Keychain (`lorian-cli.device`) | Oui (keychain natif) |
| `expires_at` | Keychain (`lorian-cli.expires_at`) | Non (timestamp) |
| `user.email` | Keychain (`lorian-cli.user_email`) | Non |
| `user.id` | Keychain (`lorian-cli.user_id`) | Non |
| `mfa_secret` | Keychain (`lorian-cli.mfa_secret`) | Oui (keychain natif) |

#### Sécurité

- **Plus jamais** de credentials en clair sur disque
- Session **à jeton unique** : le token Bearer est rafraîchi à l'expiration — par `POST /oauth/token`
  (`grant_type=refresh_token`, rotation) quand la session provient du flux OAuth `liora auth`,
  sinon par `POST /api/auth/sessions/refresh` (session legacy)
- Le `mfa_secret` (si TOTP enrollment local) est chiffré dans le keychain
- Après 5 échecs de connexion → temporaire (5 min) avec message clair
- **Mode CI / headless** : les prompts sont alimentés par `LIORIAN_CLI_CONNECT_EMAIL`,
  `LIORIAN_CLI_CONNECT_PASSWORD` et `LIORIAN_CLI_MFA_CODE` (même pattern que `LIORIAN_CLI_YES`)

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

### 5.4 `liora disconnect`

#### Purpose

Supprimer toutes les credentials stockées et déconnecter le développeur.

#### Comportement

1. **Vérifier si connecté** : credentials présentes dans le keychain
   - Si non connecté → message informatif, rien à faire
2. **Demander confirmation** ( Bubbletea confirm )
3. **Supprimer** toutes les entrées du keychain :
   - `lorian-cli.access_token`
   - `lorian-cli.mfa_token`
   - `lorian-cli.device`
   - `lorian-cli.expires_at`
   - `lorian-cli.user_email`
   - `lorian-cli.user_id`
   - `lorian-cli.mfa_secret`
   - `lorian-cli.oauth_refresh_token`
4. **Invalider le token** côté serveur (`POST /api/auth/logout`, best-effort)
5. **Afficher confirmation**

#### Sortie TUI

```
? Confirmer la déconnexion : Oui
  ✓ Déconnecté avec succès
    Toutes les credentials ont été supprimées.
```

---

### 5.5 `liora pack`

#### Purpose

Construire le build d'un module et créer une archive `.liozip` compressée.

#### Comportement

1. **Identifier le module** :
   - Si un argument `<module>` est fourni → l'utiliser
   - Sinon → lister les modules dans `library/modules/` via un sélecteur Bubbletea
2. **Vérifier l'existence** du module et de ses fichiers essentiels (`manifest.json`, `index.tsx`)
3. **Valider le `manifest.json`** (champs requis : `id`, `name`, `version`, `token`)
4. **Construire les chemins** :
   - Source module : `library/modules/<module>/`
   - Source page : `src/app/<module.uri>/` (le `uri` du manifeste, sans `/` initial)
   - Source assets : `public/assets/<module>/` (si existe)
   - Destination : `.lorian/build/`
5. **Créer l'archive ZIP** :
   - Nom : `<module>-<version>.liozip` (le `.liozip` est un ZIP renommé, extension canonique ADR-003 ; `.SenMod`/`.smp` restent lus en legacy, jamais produits)
   - Contenu : dossiers `library/modules/<module>/` + `public/assets/<module>/` et `src/app/<module.uri>` (si existe)
   - Préfixe dans l'archive : `library/modules/<module>/` + `public/assets/<module>/` et `src/app/<module.uri>`
6. **Déplacer** l'archive vers `.lorian/build/`
7. **Afficher le résumé** : taille de l'archive, emplacement

#### Structure de l'archive `.liozip`

```
<archive>.liozip (ZIP)
├── library/modules/<module>/
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

#### Flags

| Flag | Description |
|------|-------------|
| `--out <path>` | écrire l'archive vers un chemin personnalisé (ex. `--out acme-crm.liozip`, flux release §16) |
| `--version <semver>` | forcer la version du build (prioritaire sur le suffixe `@<version>`) |

#### Contraintes

- Le dossier `.lorian/build/` est créé automatiquement s'il n'existe pas
- Si une archive du même nom existe → demander confirmation (écraser)
- Le `manifest.json` doit être valide avant le pack
- Quotas d'archive (spec installation NFR-006..008, audit fail-closed §7.2) : 50 MB max, 5 000 entrées max, ratio de décompression max 100:1, 10 Mo par fichier ; symlinks et exécutables (`.so`, `.dll`, `.exe`, `.sh`, …) refusés
- Le `manifest.json` canonique est embarqué à la racine de l'archive (TECH-002) ; le résumé affiche les empreintes SHA-256 (archive + manifeste) et le nombre de fichiers

#### Sortie TUI

```
? Sélectionner le module : blog-manager
  ⠋ Validation du manifest.json...
  ⠋ Construction de l'archive...
  ⠋ Déplacement vers .lorian/build/

  ✓ Archive créée avec succès
    Module : blog-manager v0.1.0
    Fichier : .lorian/build/blog-manager-0.1.0.liozip
    Taille : 12.4 KB
```

---

### 5.6 `liora publish`

#### Purpose

Construire et publier un module dans le store via l'API `liorian-connect`.

#### Comportement

1. **Vérifier l'authentification** : token Bearer valide dans le keychain
   - Si non connecté → `liora connect` automatique
2. **Identifier le module** : sélecteur Bubbletea si non fourni
3. **Compléter le `manifest.json`** :
   - **Résoudre l'identité développeur** (jamais saisie manuellement) :
     `GET /api/developer-store/accounts/me` → `{id, name}`. `publisher.id` est
     l'identifiant de compte Liorian que `liorian-connect` utilise pour scoper
     les ressources (`DeveloperProduct.developerId`, extrait du JWT de session
     par `DeveloperAuthMiddleware`) — **ce n'est ni l'identifiant Apple ni celui
     d'un autre constructeur**, et c'est le serveur qui l'attribue. Repli sur
     l'`id` de session (keychain) si l'endpoint est injoignable. Le bloc
     `publisher` n'étant pas transmis au store (`CreateModuleProductDto` ne
     l'expose pas), il reste une métadonnée locale alignée sur le compte
     distant — résolu, pas demandé.
   - Si les métadonnées restantes sont incomplètes (champs vides) → **demander** :
     - `name` : nom affiché du module
     - `description` : description courte
     - `publisher.name` : nom affiché du développeur (uniquement si non résolu)
   - Proposer de mettre à jour le `manifest.json` local
4. **Exécuter `liora pack`** en interne (construction de l'archive)
5. **Envoyer l'archive** à l'API developer-store (`liorian-api-connect`, §8.2 ;
   `docs/specs/applications/liorian-connect.md`) en 3 étapes :
   - Résoudre le **produit module** : réutiliser le produit lié (`manifest.token`) sinon le créer
     (`POST /api/developer-store/modules` — `{name, slug, type, primaryCategory, token,
     description, icon, secondaryCategory?}` ; catégorie primaire = `manifest.category`, repli `SYSTEM`)
   - Créer la **version** (`POST /api/developer-store/modules/:id/versions` —
     `{versionString, buildNumber, …}` ; `buildNumber` = dernière build + 1, résolue via
     `GET …/versions`)
   - **Déclarer l'artefact** (`POST /api/developer-store/modules/:id/versions/:versionId/artifact` —
     `manifest` JSON, `checksum` SHA-256 hex, `manifestChecksum` (SHA-256 du manifeste canonique), `signature` base64 (charge utile canonique §7.1), `signatureKeyId` (fingerprint), `size`) — signature **obligatoire** (ADR-010, `--allow-unsigned` en dev uniquement)
   - Headers : `Authorization: Bearer <token>`
6. **Gérer la réponse** :
   - **Succès** → afficher l'URL du module dans le store
   - **Conflit** (version existante) → demander si bump de version souhaité
   - **Erreur** → afficher le message d'erreur détaillé
7. **Mettre à jour le `manifest.json`** local avec la version publiée et le token distant résolu

#### Flags

| Flag | Description |
|------|-------------|
| `--file <archive.liozip>` | publier une archive pré-construite au lieu d'emballer (flux release §16) |
| `--version <semver>` | figer la version publiée (met à jour `manifest.json`) |
| `--allow-unsigned` | publier sans signature (dev uniquement — le store refuse les artefacts non signés, ADR-010) |

#### Contraintes

- L'authentification est **obligatoire**
- La signature Ed25519 de la charge utile canonique est **obligatoire** (ADR-010) : sans clé (`liora sign keygen`), la publication échoue sauf `--allow-unsigned` explicite
- La version doit être supérieure à la dernière version publiée (SemVer)
- Le module doit passer l'audit (`liora audit`) avant la publication
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
  URL : https://store.liorian.dev/modules/blog-manager
```

#### Session de release (spec installation §16)

```text
$ liora pack ./acme-crm --version 1.3.0 --out acme-crm.liozip
$ liora sign  acme-crm.liozip --key ~/.acme/ed25519
$ liora publish mod.acme.crm --version 1.3.0 --file acme-crm.liozip
  → 201 · signatureKeyId (fingerprint) · checksum SHA-256 · manifestChecksum · état READY
  → review → APPROVED → RELEASED → distribution catalogue (outbox)
```

---

### 5.7 `liora link`

#### Purpose

Lier un module créé dans `liorian-connect` avec le module en local, via son token produit.

#### Comportement

1. **Vérifier le contexte projet** (racine + `library/modules/`) et l'authentification (sinon → `liora connect`)
2. **Sélectionner le module local** : argument positionnel ou sélecteur Bubbletea
3. **Lister les modules en ligne** via API `GET /api/developer-store/modules`, proposer une sélection
   (items au format `token — name vversion`)
4. **Résoudre le token distant** :
   - En mode CI (non-interactif) : `liora link <module> <token>` en arguments
   - En interactif : choix dans la liste
5. **Valider le token** via `GET /api/developer-store/modules/<id>` (existe + appartient au développeur)
6. **Mettre à jour le `manifest.json` local** : `token` remplacé par le token distant, métadonnées
   distantes fusionnées dans les champs absents (`name`, `description`, `publisher.*`)
7. **Persister l'état** dans `.lorian/links.json` (`{"modules": {"<module>": "<token>"}}`) —
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
    Local : library/modules/blog-manager/
    Distant : m_abc123def456 (Blog Manager v0.1.0)
```

---

### 5.8 `liora unlink`

#### Purpose

Délier un module local de son correspondant dans `liorian-connect`.

#### Comportement

1. **Vérifier le contexte projet**
2. **Lister les modules localement liés** via `.lorian/links.json` (source de vérité) + les
   manifests portant un token non-UUID (migration)
3. **Sélectionner le module à délier** : argument positionnel ou sélecteur Bubbletea
4. **Afficher le lien actuel** (token + version distante)
5. **Optionnel** : `--sync-remote` synchronise d'abord les métadonnées locales vers le produit
   distant (`PUT /api/developer-store/modules/:id`, best-effort)
6. **Demander confirmation** (Bubbletea confirm ; non-interactif → exécution directe)
7. **Délier** : régénère un **nouveau token UUID local** dans `manifest.json` et retire l'entrée de
   `.lorian/links.json`
8. **Afficher confirmation**

#### Sortie TUI

```
? Sélectionner le module à délier : blog-manager
  Actuellement lié à : m_abc123def456 (Blog Manager v0.1.0)

? Confirmer la déliaison : Oui
  ✓ Module délié avec succès
    library/modules/blog-manager/ n'est plus lié à un module distant.
```

---

### 5.9 `liora debug <module>`

#### Purpose

Lancer le debug d'un ou tous les modules dans `library/modules/` : validation puis build réel du
module, avec une **trace pas-à-pas** des étapes exécutées, la **sortie de build en temps réel** et
un **récapitulatif de sévérité** en fin d'exécution.

#### Comportement

1. **Analyser l'argument** :
   - Si `<module>` est fourni → debug uniquement ce module
   - Sinon → debug **tous** les modules dans `library/modules/` (chaque module est introduit par
     une ligne `Module <nom>`)
2. **Valider le module** (mêmes règles que `audit`) et rapporter l'étape « Validation du module »
   avec le décompte `N erreur(s), M avertissement(s)` ; chaque règle en échec alimente le
   récapitulatif par sa propre sévérité. Si erreurs → statut `ERROR` et arrêt du module.
3. **Détecter le gestionnaire de paquets** (bun → pnpm → yarn → npm) et le rapporter ; aucun →
   statut `WARNING` (étape en avertissement).
4. **Résoudre la commande de build** (étape rapportée, `NOTICE` lorsque c'est un repli) :
   - Script du `package.json` du module puis du projet (candidats `debug`, `dev`, `build`,
     comparés par clé exacte, pas de collision de sous-chaîne)
   - Repli : **bundle réel** via un bundler résolvable (`esbuild`, `tsup` — `node_modules` du
     module → `node_modules` racine → PATH) qui compile l'entrée du module dans `dist/`
   - Repli : **type-check TypeScript réel** `tsc --noEmit` si le module contient des sources
     `.ts`/`.tsx` et qu'un `tsconfig.json` + un `tsc` résolvable existent
5. **Exécuter la commande** sous une étape dédiée `RUNNING` qui se met à jour **en place** : elle
   affiche le sous-texte actif et diffuse la queue de sortie (`stdout`/`stderr`, 8 dernières lignes)
   en temps réel, puis bascule vers un statut terminal :
   - succès → `SUCCESS` (sortie conservée dans les logs)
   - script `debug`/`dev` (serveur dev / watcher) démarré → `NOTICE` (arrêté après sa fenêtre)
   - échec → `ERROR` avec la première ligne d'erreur en détail et la sortie complète dans les logs
6. **Fenêtres d'exécution** (`--timeout`, `0` = auto) :
   - un script `debug`/`dev` qui ne se termine jamais est arrêté après une **fenêtre de démarrage**
     (15 s par défaut) et signalé comme démarré (mode serveur/watch)
   - un build one-shot est plafonné (5 min par défaut) ; le dépassement produit un `ERROR`
7. **Aucun build ni type-check possible** → statut `WARNING` (`no_build_script`), jamais un faux « OK »
8. **Annulation** : `Ctrl+C` (ou `Esc` en interactif, `SIGINT` en non-interactif) interrompt
   l'exécution, arrête l'**arbre de process** du build (groupe de process dédié, SIGINT puis
   SIGKILL) et affiche une carte de confirmation ; code de sortie **`130`**.
9. **Mode all modules** : itérer sur chaque module et afficher le tableau de statut (nom, statut,
   erreurs), les logs par module puis le récapitulatif global.
10. **Récapitulatif de sévérité** : chaque exécution se clôt par un bloc `Summary` — succès,
    notice(s), avertissement(s), erreur(s), obsolète(s) (les étapes `RUNNING` ne sont pas comptées).

#### Flags

| Flag | Défaut | Description |
|------|--------|-------------|
| `--timeout` | `0` (auto) | Fenêtre d'exécution d'un script `debug`/`dev`, ou plafond d'un build one-shot |

#### Vocabulaire d'étapes

Le même vocabulaire d'étapes (`internal/tui/step.go`) est partagé par toute la CLI : `RUNNING`,
`SUCCESS`, `NOTICE`, `WARNING`, `ERROR`, `DEPRECATED`. Une étape portant un identifiant peut être
rapportée plusieurs fois — la vue live la met à jour en place (passage `RUNNING` → statut terminal
avec tail de sortie).

#### Sortie TUI (single)

```
  Debugging: com.example.blog-manager
  ✓ Module validation — 0 error(s), 0 warning(s)
  ✓ Package manager detection — bun detected
  ◆ Build command resolution — bun run build
  ⠋ Build execution — bun run build
      [vite] building for production...
      [vite] ✓ 42 modules transformed
  ✓ Build execution — bun run build

  ┌──────────────────────────────────────────┐
  │ Module : com.example.blog-manager        │
  │ Status : ✓ OK                            │
  └──────────────────────────────────────────┘

  Summary
  ✓ 4 success
  ✓ 0 warning(s)
  ✗ 0 error(s)
```

> Un script `debug`/`dev` (serveur dev) s'affiche en `NOTICE` :
> `⊘ Build execution — dev script running — stopped after 15s (server/watch mode)`.

#### Sortie TUI (annulation)

```
  Debugging: com.example.blog-manager
  ✓ Module validation — 0 error(s), 0 warning(s)
  ✓ Package manager detection — bun detected
  ◆ Build command resolution — bun run build
  ⚠ Cancellation
  ⊘ Cancelled by the developer (Ctrl+C)
```

#### Sortie TUI (all)

```
  Debugging all modules
  Module com.example.blog-manager
  ✓ Module validation — 0 error(s), 0 warning(s)
  ...
  Module com.example.billing
  ...
  ┌──────────────────────────────┬──────────┬─────────────┐
  │ Module                       │ Statut   │ Erreurs     │
  ├──────────────────────────────┼──────────┼─────────────┤
  │ com.example.blog-manager     │ ✓ OK     │ 0           │
  │ com.example.billing          │ ✓ OK     │ 0           │
  │ com.example.calendar         │ ⚠ WARNING│ 2           │
  │ com.example.crm              │ ✓ OK     │ 0           │
  └──────────────────────────────┴──────────┴─────────────┘

  Summary
  ✓ 12 success
  ⊘ 1 notice(s)
  ⚠ 3 warning(s)
  ✗ 0 error(s)
```

---

### 5.10 `liora audit <module>`

#### Purpose

Auditer la conformité d'un ou tous les modules par rapport aux règles du système Liora.

#### Comportement

1. **Analyser l'argument** :
   - Si `<module>` est fourni → audit uniquement ce module
   - Sinon → audit **tous** les modules dans `library/modules/`
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
| **manifest.json** | Champ `domain` au format reverse-DNS (`com.organization.domain` ; le préfixe `mod.liorian.` n'est plus requis) | WARNING |
| **manifest.json** | Le `domain` correspond au dossier du module (`library/modules/<domain>`) | WARNING |
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
| **requirements** | Les requirements listées existent dans `library/modules/` **ou** `src/modules/` (exceptions : modules core plateforme `organization`, `identity`) | ERROR |
| **dependencies** | Les dépendances npm déclarées dans le `package.json` du module sont installées dans `node_modules` | ERROR |
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

Chaque module est introduit par un en-tête avec un verdict aligné à droite
(`✓ passed` / `✗ failed`), suivi du tableau des **points d'action** (ERROR /
WARNING uniquement, colorés par sévérité) puis d'un décompte compact. Les
contrôles réussis sont résumés (« N check(s) passed ») au lieu d'encombrer le
tableau. Un module sans aucun point d'action se termine par la carte de succès.

```
  Audit: mod.liorian.blog-manager                          ✗ failed
  ────────────────────────────────────────────────────────────────
  ┌────────────────┬─────────────┬──────────────────────────────┐
  │ Catégorie      │ Règle       │ Problème                     │
  ├────────────────┼─────────────┼──────────────────────────────┤
  │ index.tsx      │ export      │ ✗ export not compliant       │
  │ requirements   │ organization│ ⚠ organization not compliant │
  └────────────────┴─────────────┴──────────────────────────────┘
  ✓ 25 check(s) passed · ⚠ 1 warning(s) · ✗ 1 error(s)

  Résumé : 1 erreur(s), 1 avertissement(s)
```

Un module conforme affiche l'en-tête `✓ passed`, la ligne des contrôles
réussis, puis la carte `✓ Audit passed — no errors, no warnings`.

---

### 5.11 `liora help`

#### Purpose

Afficher l'aide contextuelle de la CLI.

#### Comportement

1. **Sans argument** : afficher la liste de toutes les commandes avec descriptions
2. **Avec une commande** : afficher l'aide détaillée de cette commande (flags, exemples)

#### Sortie TUI (sans argument)

```
┌───────────────┐
│ ⬢ liora   │   ← wordmark (badge brand, dégrade en texte sous --no-color)
└───────────────┘

Liora CLI — Development tool for Liora modules

Usage:
  liora [command]

Available Commands:
  audit       Audit a module's conformance
  auth        Authenticate via OAuth2 (browser)
  build       Build the application for production (project `build` script)
  check       Run the project checks (project `lint` script)
  completion  Generate the autocompletion script for the specified shell
  connect     Connect to Liora Connect
  create      Create a new module
  debug       Debug a module or all modules
  dev         Start the development server (project `dev` script)
  disconnect  Disconnect
  help        Help about any command
  init        Initialize a new Liora project
  link        Link a local module to a remote module
  marketplace Search and install modules from the public catalog
  pack        Build a module archive (.liozip)
  publish     Publish a module to the store
  repair      Repair a module's audit failures
  sign        Sign .liozip archives (Ed25519)
  start       Start the built application (project `start` script)
  test        Run the tests of a module or all modules
  unlink      Unlink a local module from liorian-connect

Flags:
      --help     help for liora
  -v, --version  version for liora

Global Flags:
      --lang string    language / UI locale (fr-FR, en-US, …)
      --no-color       disable colors
      --verbose        enable verbose logs

Use "liora [command] --help" for more information about a command.

Exemples:
  liora init
  liora create module
  liora connect
  liora pack blog-manager
  liora sign blog-manager
  liora publish blog-manager
  liora audit
```

> Le help est thématisé : labels de section teintés (accent), wordmark brand en tête ;
> les informations et messages — y compris l'aide — sont localisés (NFR-007).

---

### 5.12 `liora -v` / `liora --version`

#### Purpose

Afficher la version actuelle de la CLI.

#### Comportement

1. Lire la version compilée dans le binaire — alignée sur `app.config.json` (`main.version`, `main.branch`, `main.commit`, `main.date`), surchargée par `ldflags` au build de release
2. Afficher : `liora v<version> (<os>/<arch>) <branch> (<commit>)` (template de version Cobra)

#### Sortie

```
liora v0.20.0 (darwin/arm64) alpha (e7cbd2f)
```

---

### 5.13 `liora sign`

#### Purpose

Gérer les signatures numériques Ed25519 des modules : générer des clés, signer les archives `.liozip`
et vérifier les signatures. La signature garantit l'intégrité et l'authenticité des modules
avant publication.

#### Sous-commandes

| Sous-commande | Description |
|---------------|-------------|
| `liora sign keygen` | Générer une paire de clés Ed25519 et la stocker dans le keychain |
| `liora sign <module>` | Signer l'archive `.liozip` d'un module |
| `liora sign verify <module\|archive>` | Vérifier la signature d'un module |

---

##### 5.13.1 `liora sign keygen`

###### Comportement

1. **Vérifier si des clés existent déjà** dans le keychain
   - Si oui → afficher le fingerprint de la clé publique et demander régénération
2. **Générer une paire de clés Ed25519** (`crypto/ed25519`)
3. **Stocker** la clé privée et la clé publique dans le keychain système
   - Clé privée : `lorian-cli-signing.signing_private_key`
   - Clé publique : `lorian-cli-signing.signing_public_key`
   - Fallback : fichier chiffré `~/.lorian-cli/signing.enc` (AES-256-GCM)
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

##### 5.13.2 `liora sign <module>`

###### Comportement

1. **Vérifier le contexte** : être à la racine d'un projet Liora
2. **Identifier le module** : argument `<module>` ou sélecteur Bubbletea
3. **Charger le `manifest.json`** du module pour obtenir la version
4. **Vérifier que l'archive `.liozip` existe** dans `.lorian/build/` (`.SenMod`/`.smp` historiques acceptés en lecture)
   - Si absente → erreur avec suggestion d'exécuter `liora pack <module>`
5. **Charger la clé privée** depuis le keychain
   - Si absente → erreur avec suggestion d'exécuter `liora sign keygen`
6. **Signer l'archive** :
   - Calculer l'empreinte SHA-256 de l'archive et l'empreinte du manifeste canonique, construire la charge utile canonique `{moduleIdentifier, version, checksum, manifestChecksum, entry, type}` (spec installation §7.1)
   - Signer avec `ed25519.Sign(privateKey, payload)`
   - Écrire la signature dans `<archive>.sig` (même dossier que l'archive)
7. **Afficher le résumé** : module, version, fingerprint du signataire, chemin du `.sig`

###### Contraintes

- L'archive `.liozip` doit exister (résultat de `liora pack`)
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
    Archive  : .lorian/build/blog-manager-0.1.0.liozip
    Signature : .lorian/build/blog-manager-0.1.0.liozip.sig
    Signataire : a1b2c3d4... (fingerprint SHA-256)
```

---

##### 5.13.3 `liora sign verify <module>`

###### Comportement

1. **Vérifier le contexte** : être à la racine d'un projet Liora
2. **Identifier le module** : argument `<module>` ou sélecteur Bubbletea
3. **Charger le `manifest.json`** du module pour obtenir la version
4. **Vérifier que l'archive `.liozip` et le fichier `.sig` existent**
5. **Charger la clé publique** depuis le keychain
   - Si absente → erreur avec suggestion d'exécuter `liora sign keygen`
6. **Vérifier la signature** :
   - Recalculer la charge utile canonique (empreintes sur les octets reçus) et vérifier avec `ed25519.Verify(publicKey, payload, signature)` ; repli migration : signature historique sur octets bruts (signalée `legacy`)
7. **Afficher le résultat** : ✓ Signature valide ou ✗ Signature invalide

###### Contraintes

- L'archive `.liozip` ET le fichier `.sig` doivent exister
- La clé publique doit exister dans le keychain
- En cas de signature invalide → afficher un message d'erreur explicite (possiblement archive corrompue ou clé incorrecte)

###### Sortie TUI (valide)

```
? Sélectionner le module : blog-manager
  ⠋ Vérification de la signature…

  ✓ Signature valide
    Module    : blog-manager v0.1.0
    Archive   : .lorian/build/blog-manager-0.1.0.liozip
    Signataire : a1b2c3d4...
```

###### Sortie TUI (invalide)

```
? Sélectionner le module : blog-manager
  ⠋ Vérification de la signature…

  ✗ Signature invalide
    Module   : blog-manager v0.1.0
    Archive  : .lorian/build/blog-manager-0.1.0.liozip
    → L'archive a pu être modifiée ou la clé de vérification est incorrecte.
```

---

### 5.14 `liora auth`

#### Purpose

Authentifier le développeur via le flux OAuth2 **code d'autorisation + PKCE** (RFC 7636),
en passant par le navigateur. Complément du `liora connect` (email/mot de passe), le
flux ouvre la page d'autorisation de `liorian-auth`, reçoit la redirection sur un serveur
local en boucle, échange le code contre des jetons, puis stocke la session de façon
sécurisée.

#### Comportement

1. **Vérifier si déjà connecté** : credentials existantes dans le keychain — afficher le
   statut et proposer la reconnexion (comme `connect`)
2. **Résoudre la configuration OAuth** depuis l'entrée `oauth` de `liorian-auth` dans
   `app.config.json` : `authorizationEndpoint`, `tokenEndpoint`, `revokeEndpoint`,
   `clientId`, `scopes` (défauts : `/oauth/authorize`, `/oauth/token`, `/oauth/revoke`,
   client `lior-cli`, scopes `openid profile email`)
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

Le code d'autorisation est fourni via la variable d'environnement `LIORIAN_CLI_AUTH_CODE`
(même pattern que `LIORIAN_CLI_YES`) : la CLI saute l'étape navigateur + serveur local et
échange directement le code. Sans code en mode non interactif, la commande échoue avec une
erreur catégorisée (exit 2).

#### Sortie TUI

```
  Ouverture de la page d'autorisation dans votre navigateur…
  https://auth.liorian.protorians.com/oauth/authorize?response_type=code&…

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

### 5.15 `liora test <module>`

#### Purpose

Exécuter les tests d'un ou tous les modules dans `library/modules/` : validation puis exécution
de la suite de tests, avec la même **trace pas-à-pas** que le debug, la **sortie de test en temps
réel** et un **récapitulatif de sévérité** en fin d'exécution. L'exécution se termine avec un
**code de sortie non nul** dès qu'un module voit ses tests en échec.

#### Comportement

1. **Analyser l'argument** :
   - Si `<module>` est fourni → tests de ce module uniquement
   - Sinon → tests de **tous** les modules dans `library/modules/` (chaque module est introduit
     par une ligne `Module <nom>`) ; le run global échoue (exit **`13`**) si au moins un module
     échoue
2. **Valider le module** (mêmes règles que `audit`) et rapporter l'étape « Validation du module »
   avec le décompte `N erreur(s), M avertissement(s)` ; chaque règle en échec alimente le
   récapitulatif par sa propre sévérité. Si erreurs → statut `ERROR` et arrêt du module.
3. **Résoudre le gestionnaire de paquets** : celui **choisi à l'installation** (`project.packageManager`
   écrit par `liora init`), puis une surcharge `test.packageManager`, puis la détection PATH
   (bun → pnpm → yarn → npm) ; l'étape rapporte la source (« choisi à l'installation » ou
   « détecté »). Aucun → statut `WARNING` (étape en avertissement).
4. **Résoudre le package de test** (étape rapportée), par ordre de priorité :
   - Flag `--runner`, puis `test.modules.<module>.runner` / `test.runner` du
     `lorian.config.json` ; un package absent est **installé en dépendance de développement**
     via le gestionnaire (périmètre du gestionnaire : `bun add -d`, `pnpm/yarn add -D`,
     `npm install -D`). Les sentinelles `script` (script `package.json`) et `builtin` (ex.
     `bun test`) sont acceptées.
   - Script `test` du `package.json` du module puis du projet (comparé par clé exacte)
   - Repli : **package de test installé** du catalogue principal (`vitest run`,
     `jest --ci --runInBand`, `mocha`, `ava` — `node_modules` du module → `node_modules` racine →
     PATH), puis **runner intégré** (`bun test`) lorsque le gestionnaire est `bun`, **uniquement si
     le module contient des fichiers ou dossiers de test** (fichiers `*.test.*` / `*.spec.*`, ou
     dossier conventionnel `__tests__/`, `test/`, `tests/`, `spec/`, `specs/`)
   - Si aucun package n'est disponible, le développeur **choisit** parmi le catalogue (avec
     l'état d'installation) ou saisit un package personnalisé, puis valide son installation
     (mode interactif uniquement) ; le choix est persisté
   - Aucun script ni runner ni fichier de test → statut `WARNING` (`no_test_script`, avec la liste
     des packages disponibles), jamais un faux « OK »
   - Un module **sans fichier ni dossier de test** (`*.test.*`, `*.spec.*`, ou dossier
     `__tests__/`, `test/`, `tests/`, `spec/`, `specs/`) est **ignoré** (statut `SKIPPED`, étape
     `NOTICE`, `test.no_tests`) même lorsqu'un script ou un runner est configuré : la commande n'est
     **pas** lancée, ce qui évite l'échec « No test files found »
5. **Persister** le package de test résolu (et le gestionnaire utilisé) dans la section `test` de
   `lorian.config.json`, pour que les exécutions suivantes n'aient plus à détecter/choisir.
6. **Exécuter la commande** sous une étape dédiée `RUNNING` qui se met à jour **en place** : elle
   affiche le sous-texte actif et diffuse la queue de sortie (`stdout`/`stderr`, 8 dernières
   lignes) en temps réel, puis bascule vers un statut terminal :
   - succès (code 0) → `SUCCESS` (sortie conservée dans les logs)
   - échec (code ≠ 0) → `ERROR` avec la première ligne d'erreur en détail et la sortie complète
     dans les logs
7. **Plafond d'exécution** (`--timeout`, `0` = auto) : une suite bloquée est arrêtée après **2 min**
   par défaut ; le dépassement produit un `ERROR`.
8. **Annulation** : `Ctrl+C` (ou `Esc` en interactif, `SIGINT` en non-interactif) interrompt
   l'exécution, arrête l'**arbre de process** de la suite (groupe de process dédié) et affiche une
   carte de confirmation ; code de sortie **`130`**.
9. **Mode all modules** : itérer sur chaque module et afficher le tableau de statut (nom, statut,
   erreurs), les logs par module puis le récapitulatif global.
10. **Récapitulatif de sévérité** : chaque exécution se clôt par un bloc `Summary` — succès,
    notice(s), avertissement(s), erreur(s), obsolète(s) (les étapes `RUNNING` ne sont pas comptées).

#### Flags

| Flag | Défaut | Description |
|------|--------|-------------|
| `--timeout` | `0` (auto) | Plafond d'exécution d'une suite de tests (2 min par défaut) |
| `--runner` | `""` | Package de test imposé (persisté dans `lorian.config.json`) |

#### Vocabulaire d'étapes

Identique à la section 5.9 : `RUNNING`, `SUCCESS`, `NOTICE`, `WARNING`, `ERROR`, `DEPRECATED`,
avec mise à jour en place des étapes portant un identifiant.

#### Sortie TUI (single)

```
  Testing: com.example.blog-manager
  ✓ Module validation — 0 error(s), 0 warning(s)
  ✓ Package manager detection — bun (chosen at install)
  ✓ Test package — vitest configured
  ✓ Test command resolution — bun run test
  ⠋ Test execution — bun run test
      ✓ tests 1 passed (12ms)
      ✓ File: index.spec.tsx
  ✓ Test execution — bun run test

  ┌──────────────────────────────────────────┐
  │ Module : com.example.blog-manager        │
  │ Status : ✓ OK                            │
  │ Runner : vitest                          │
  │ Command : bun run test                   │
  └──────────────────────────────────────────┘

  Summary
  ✓ 5 success
  ✓ 0 warning(s)
  ✗ 0 error(s)
```

> Une suite en échec s'affiche en `ERROR` :
> `✗ Test execution — exit status 1` (nouvelle ligne du détail), puis la carte de statut montre
> `Status : ✗ ERROR` et la commande se termine avec le code de sortie **`13`**.

---

### 5.16 `liora create view <module> [name]`

#### Purpose

Générer une nouvelle **vue de présentation** dans un module existant, à
`library/modules/<module>/presentation/views/<name>.view.tsx`, à partir du mockup de vue
embarqué (`view.tsx`) renommé : composant React (`<Name>View`), titre affiché et description.
FR-030.

#### Comportement

1. **Vérifier le contexte** : être à la racine d'un projet Liora
2. **Résoudre le module cible** : argument positionnel `[module]`, sinon sélection interactive
   parmi les modules de `library/modules/`
3. **Résoudre l'identifiant de vue** : `--name`, ou second argument positionnel, sinon prompt
   (défaut `my-view`) ; validé en **kebab-case** (`ValidateName`)
4. **Compléter les métadonnées** : titre (`--label`, défaut : identifiant en Title Case) et
   description (`--description`, optionnelle)
5. **Scaffolder** depuis le mockup embarqué (surchargeable par `--mockup` ou
   `LIORIAN_VIEW_MOCKUP`) : le corps est réécrit pour renommer le composant et ses libellés
6. **Afficher** le chemin relatif créé et le nom du composant

#### Flags

| Flag | Description |
|------|-------------|
| `--mockup string` | fichier de vue de référence (défaut : mockup embarqué) |
| `--name string` | identifiant de la vue (kebab-case) |
| `--label string` | titre affiché par la vue (défaut : identifiant en Title Case) |
| `--description string` | description affichée par la vue (optionnelle) |

#### Contraintes

- Le module doit exister dans `library/modules/` (sinon erreur catégorisée)
- Une vue existante au même nom n'est **jamais écrasée** (erreur explicite)
- En mode non-interactif, `--name` (ou l'argument positionnel) est obligatoire

#### Sortie TUI

```
  ✓ Created library/modules/com.example.blog-manager/presentation/views/user-profile.view.tsx
  ✓ Component: UserProfileView
```

---

### 5.17 `liora repair [module]`

#### Purpose

Réparer automatiquement les anomalies **bloquantes** signalées par `liora audit` sur un module
(ou tous), puis re-auditer ; les points qui exigent une modification de code sont restitués en
**instructions pas à pas**. FR-031.

#### Comportement

1. **Vérifier le contexte** : être à la racine d'un projet Liora
2. **Analyser** le(s) module(s) via le pipeline d'audit
3. **Appliquer les correctifs automatiques** :
   - champs de manifeste (`id`, `name`, `version`, `token`, `entry`, `permissions`, `platforms`,
     `compatibility`, `capabilities`, `category`, `domain`)
   - JSON malformé
   - dépendances npm manquantes (installation via le gestionnaire, sauf `--no-install`)
   - nom du dossier du module : proposé via un prompt `tab`-pour-remplir, appliqué directement
     avec `--no-interaction`
4. **Re-auditer** le module et restituer les points non réparables en instructions
5. **Afficher le rapport** : en-tête de verdict, tableau des correctifs appliqués, instructions
   manuelles, puis carte de synthèse ; code de sortie non nul s'il reste des erreurs bloquantes

#### Flags

| Flag | Défaut | Description |
|------|--------|-------------|
| `--dry-run` | `false` | montrer les correctifs sans les écrire |
| `--warnings` | `false` | réparer aussi les avertissements (non bloquants) |
| `--no-install` | `false` | ne pas lancer le gestionnaire de paquets pour les dépendances manquantes |
| `--no-interaction` | `false` | ne poser aucune question (les noms de dossier suggérés sont appliqués) |
| `--output` | `table` | format de sortie (`table` ou `json`) |

#### Sortie TUI

```
  Repair: com.example.blog-manager                                    ✓ fixed
  Fixes
  ┌──────────┬──────────────────┬─────────────────────────────┐
  │ Category │ Rule             │ Fix                         │
  ├──────────┼──────────────────┼─────────────────────────────┤
  │ manifest │ version          │ ✓ set version to "0.0.0"    │
  │ manifest │ token            │ ✓ generated a new UUID token│
  └──────────┴──────────────────┴─────────────────────────────┘

  ┌──────────────────────────────────────────┐
  │ Summary                                  │
  │ ✓ 2 automatic fix(es) applied            │
  └──────────────────────────────────────────┘
```

---

### 5.18 `liora dev|build|start|check`

L'outillage applicatif (`dev`, `build`, `start`, `check`) proxie les scripts `package.json` du
projet via le gestionnaire de paquets choisi à l'installation, avec hooks `before`/`after` et une
**porte de santé** (audit des modules) en pre-flight pour `dev`/`build`/`start`. La spécification
complète (correspondance commandes → scripts, résolution, passthrough d'arguments, codes de
sortie, configuration `toolchain`) est dans **`docs/specs/liora-toolchain.md`** (TFC-001 →
TFC-017).

---

## 6. Modèle de données local

### 6.1 Fichier `lorian.config.json` (optionnel)

Placé à la racine du projet Liora, ce fichier permet de configurer la CLI.

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
      "com.example.blog-manager": {
        "runner": "jest"
      }
    }
  },
  "cli": {
    "lang": "fr-FR",
    "noColor": true
  }
}
```

> La configuration est **JSON uniquement** (le parser TOML a été retiré en 0.0.9). Le fichier
> historique `lorian.config.toml` ne sert plus que de marqueur de projet (racine) pour
> `create`/`link`/etc., et `lorian.config.json` est écrit par `liora init`.
> `cli.lang` force la langue d'interface (NFR-007, FR-025) ; un champ vide garde l'auto-détection
> (`LIORIAN_CLI_LANG` / locale OS). `cli.noColor` désactive les couleurs de sortie. `--lang`,
> `--no-color` et `--verbose` **persistent** leur choix dans ce fichier à chaque exécution dans le
> projet (`cli.lang`, `cli.noColor`, `debug.verbose`), la priorité restant flag → env → config.
>
> La section `test` est **écrite automatiquement** par `liora test` : `packageManager` reprend
> le gestionnaire choisi à l'installation (ou celui réellement utilisé), `runner` mémorise le
> package de test par défaut et `modules.<domaine>.runner` surcharge un module. Les valeurs
> `runner` acceptées sont un package du catalogue (`vitest`, `jest`, `mocha`, `ava`), un package
> personnalisé, `builtin` (ex. `bun test`) ou `script` (le script `test` du `package.json`).

### 6.2 Fichier `manifest.json` (par module)

Le `manifest.json` est le **contrat technique déclaratif** de chaque module (identité, plateformes,
compatibilité, permissions/scopes, capacités, activation, prérequis, interface). C'est la source
de vérité dont découlent la déclaration React (`index.tsx`, `ModuleDeclarationInterface`) et le
catalogue backend. Les dépendances npm sont portées par le `package.json` du module.

- **Schéma canonique** : `schemaVersion: 1`, publié avec le SDK
  (`@liorian/sdk`, `docs/modules/module-manifest.md`), validé en CI. La structure complète
  est décrite en §5.2 (`liora create module`).
- **Identité stable** : `id`, `domain`, `key`, `name`, `permissions` ne changent jamais (le
  catalogue, les activations et les contrôles d'accès en dépendent) ; `version` ne bouge que par
  incrément SemVer.
- **Séparation** : `requirements` et `optionalRequirements` sont déclarés **uniquement** ici
  (jamais dans `index.tsx`) ; les dépendances npm sont déclarées dans le `package.json` du module.
- **`token`** : identifiant public de liaison produit (UUID local régénéré au `unlink`, remplacé
  par l'id produit distant au `link`) — ce n'est pas un secret.

### 6.3 Keychain — Hiérarchie des clés

```
lorian-cli/
├── access_token      # Token Bearer JWT (session à jeton unique)
├── mfa_token         # Token MFA court (après vérification TOTP/recovery)
├── device            # ID du device (session connectée)
├── expires_at        # Timestamp d'expiration
├── user_id           # ID du développeur
├── user_email        # Email du développeur
├── mfa_secret        # Secret TOTP (si enrollment local)
└── oauth_refresh_token  # Refresh token OAuth2 (flux `liora auth`)

lorian-cli-signing/
├── signing_public_key   # Clé publique Ed25519 (fingerprint du développeur)
└── signing_private_key  # Clé privée Ed25519 (signature des archives .liozip)
```

---

## 7. Sécurité

### 7.1 Stockage des credentials

| Mécanisme | Plateforme | Implémentation |
|-----------|------------|----------------|
| macOS Keychain | macOS | `security` CLI ou `go-keyring` |
| Secret Service | Linux | D-Bus + `libsecret` via `go-keyring` |
| Credential Manager | Windows | `cmdkey` ou `Credential Manager` via `go-keyring` |
| Fichier chiffré (fallback) | Sans keychain | Vault AES-256-GCM `~/.lorian-cli/credentials.enc` |

- Le backend par défaut est le keychain système ; si le keychain est injoignable (probe de lecture),
  la CLI bascule **transparentement** sur un vault fichier chiffré AES-256-GCM
  (`credentials.enc` pour l'auth, `signing.enc` pour les clés).
- La clé AES du vault est dérivée en **PBKDF2** du secret machine par utilisateur
  (`~/.lorian-cli/machine.secret`) — jamais de passphrase codée en dur.
- `LIORIAN_CLI_STORE=keychain|file` force le backend (CI/headless) ; `NewStoreVolatile` isole les tests.
- Service keychain : `lorian-cli` (credentials), `lorian-cli-signing` (clés de signature).

### 7.2 Chiffrement des archives

- Les archives `.liozip` ne contiennent **jamais** de credentials, tokens ou données sensibles
- Le `manifest.json` ne contient que les métadonnées publiques du module
- Le token UUID est un identifiant public, pas un secret

### 7.3 Communication réseau

- Toutes les communications avec `liorian-connect` utilisent **HTTPS** (TLS 1.3)
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

- Si le compte développeur a la MFA activée, `liora connect` **exige** la vérification
- Le secret TOTP peut être géré côté serveur (recommandé) ou stocké localement (optionnel)
- Les backup codes sont utilisables uniquement en secours

### 7.6 Signature numérique

- Les clés de signature sont stockées dans le keychain OS (service `lorian-cli-signing`)
- La clé privée n'est **jamais** affichée à l'écran ni exportée
- Fallback : fichier chiffré `~/.lorian-cli/signing.enc` (AES-256-GCM, clé PBKDF2 du secret machine)
  quand le keychain n'est pas disponible
- L'algorithme utilisé est **Ed25519** (signatures compactes de 64 octets, clés de 32 octets)
- Les fichiers `.sig` sont des binaires contenant uniquement la signature Ed25519
- La vérification de signature utilise la clé publique stockée dans le keychain
- En cas de perte de clés, `liora sign keygen` permet de régénérer une nouvelle paire

---

## 8. Communication API

> La CLI consomme deux backends du workspace (`docs/specs/applications/module-distribution.md`) :
> `liorian-api-core` (authentification, MFA, OAuth — port `5711`) et `liorian-api-connect`
> (**Developer Store** de publication — port `5721`). Le **catalogue public** vit dans un troisième
> service, `liorian-api-store` (port `5731`, `/api/catalog/*`), non appelé directement par la CLI.
>
> Tous les endpoints sont servis derrière un préfixe global **`/api`** (hors routes OAuth publiques)
> et une enveloppe Raiton unique : `{ message, data, statusCode }`
> (`RaitonResponses(message, data, statusCode)`). Les erreurs reprennent l'enveloppe (`message`), le
> code HTTP et un `code` optionnel.
>
> La base URL du client est résolue par le **registre `app.config.json`** (TECH-009) : le binaire
> embarque le registre workspace (`liorian-auth`, `liorian-store`, `liorian-connect`, …), un
> `app.config.json` local peut le surcharger, et `LIORIAN_AUTH_API` force la base URL (priorité max).
> Le timeout HTTP par défaut est de 30 s (surchargeable par application via `api.timeout`). Le header
> `Authorization: Bearer <token>` est posé à chaque requête quand une session existe.

### 8.1 Endpoints `liorian-api-core` — authentification, MFA, OAuth

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

> Le module OAuth (`docs/specs/applications/liorian-oauth.md`) expose aussi `client_credentials`,
> `refresh_token` (rotation), `/oauth/introspect`, `/oauth/userinfo` (OIDC), `/.well-known/*` et
> l'administration des clients ; la CLI n'utilise que le sous-ensemble **code + PKCE** ci-dessus.

### 8.2 Endpoints `liorian-api-connect` — Developer Store (publication)

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
> `StoreModule*`) ; jeton `Bearer` partagé émis par `liorian-api-core` (`JWT_SECRET` aligné). Voir
> `docs/specs/applications/liorian-connect.md`.

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
| `checksum` | string | oui | SHA-256 hex de l'archive `.liozip` (recalculé serveur) |
| `signature` | string | oui | Signature Ed25519 base64 de la charge utile canonique (§7.1) |
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
| `RunWithSteps` | Trace pas-à-pas live + annulation + récapitulatif de sévérité (`tui/step.go`) | Custom (Bubbletea + `bubbles/spinner`) |
| `Select` | Sélection dans une liste (`tui.Select`) | `bubbles/list` |
| `AskText` | Saisie de texte (`tui.AskText`) | `bubbles/textinput` |
| `Confirm` | Confirmation oui/non (`tui.Confirm`) | Custom (modèle Bubbletea minimal) |
| `Table` | Affichage de données tabulaires (`tui.Table`, arrondi + bandes) | Custom (lipgloss) |
| `SummaryCard` / `Wordmark` / `StepsList` / `LogsBlock` | Cartes de synthèse, badge brand, listes d'étapes, blocs de logs (`tui/components.go`) | Custom (lipgloss) |

> Tous les composants interactifs se **dégradent en sortie non-interactive** lorsque le terminal
> n'est pas un TTY (ci / pipes) : prompts résolus via `LIORIAN_CLI_YES`, arguments positionnels,
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
    binary: liora
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
      - -X main.branch={{.Branch}}
      - -X main.commit={{.ShortCommit}}
      - -X main.date={{.Date}}

archives:
  - id: packages
    formats:
      - tar.gz
    name_template: lior-cli_{{ .Version }}_{{ .Os }}_{{ .Arch }}
    format_overrides:
      - goos: windows
        formats:
          - zip
  - id: binaries
    formats:
      - binary
    name_template: liorian_{{ .Version }}_{{ .Os }}_{{ .Arch }}

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
go install github.com/protorians/lior-cli@latest

# npm / npx (npmjs)
npm install -g @liorian/cli
# ou
npx @liorian/cli

# macOS / Linux
curl -sSL https://get.liora.dev/cli | sh

# Windows (PowerShell)
iwr -useb https://get.liora.dev/cli.ps1 | iex

# Homebrew (tap = dépôt protorians/lior-cli, formula `Formula/liora.rb`)
brew tap protorians/lior-cli https://github.com/protorians/lior-cli.git
brew install protorians/lior-cli/liora
# (après ce tap, `brew install protorians/lior-cli/liora` suffit ensuite.
#  La forme courte `brew install protorians/lior-cli` n'est PAS valide dans
#  Homebrew : une référence de tap exige 3 segments `user/repo/formula`, et
#  l'auto-tap `brew install user/repo/formula` sans `brew tap` vise le repo
#  `user/homebrew-<repo>` — d'où le `brew tap` explicite vers ce dépôt.)
```

### 10.3 Variables de compilation

| Variable | Description | Défaut |
|----------|-------------|--------|
| `main.version` | Version du binaire (alignée sur `app.config.json`) | `dev` |
| `main.branch` | Branche git (alignée sur `app.config.json`) | `none` |
| `main.commit` | Hash court du commit git (aligné sur `app.config.json`) | `none` |
| `main.date` | Date de compilation (alignée sur `app.config.json`) | `unknown` |

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
| `13` | Échec de tests |
| `130` | Opération annulée par le développeur (`Ctrl+C` / `SIGINT`, `128 + SIGINT`) |

### 11.2 Messages d'erreur

Les messages sont **localisés** (NFR-007, FR-025) : catalogues `en-US` (défaut) et `fr-FR`
embarqués dans le binaire, résolution `--lang` → `LIORIAN_CLI_LANG` → `cli.lang` → locale OS
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
  → Run 'liora connect' to sign in again.
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
`$LIORA`, pointe `LIORIAN_AUTH_API` vers la mock API (une par script), force le vault fichier
(`LIORIAN_CLI_STORE=file`), désactive l'update check (`LIORIAN_CLI_SKIP_UPDATE=1`), injecte des
fixtures portables `bun/npm/tsc/node` et donne un `HOME` isolé writable par script.

### 12.2 Scénarios de test critiques

Suite E2E réelle (16 scripts txtar) : `01_help_version`, `02_init`, `02b_init_busy`,
`03_create`, `04_pack`, `05_sign`, `06_debug`, `07_audit`, `08_network`, `09_mfa`,
`10_link_unlink`, `11_auth`, `12_test`, `13_marketplace`, `14_toolchain`, `15_repair`.

| ID | Scénario |
|----|----------|
| TC-001 | `liora init` avec bun détecté (et génération du `.env` depuis l'exemple) |
| TC-002 | `liora init` avec aucun gestionnaire détecté |
| TC-003 | `liora create module` avec nom invalide |
| TC-004 | `liora create module` avec nom valide |
| TC-005 | `liora connect` succès sans MFA |
| TC-006 | `liora connect` avec MFA TOTP |
| TC-007 | `liora connect` échec (mauvais identifiants) |
| TC-008 | `liora disconnect` avec confirmation |
| TC-009 | `liora pack` module existant |
| TC-010 | `liora pack` module avec assets |
| TC-011 | `liora publish` succès |
| TC-012 | `liora publish` version existante |
| TC-013 | `liora link` succès |
| TC-014 | `liora unlink` succès |
| TC-015 | `liora debug` module unique |
| TC-016 | `liora debug` tous les modules |
| TC-017 | `liora audit` module conforme |
| TC-018 | `liora audit` module avec erreurs |
| TC-019 | `liora help` sans argument |
| TC-020 | `liora help` avec commande |
| TC-021 | `liora -v` affiche la version |
| TC-022 | `liora sign keygen` génère et stocke les clés Ed25519 |
| TC-023 | `liora sign <module\|archive>` signe la charge utile canonique et produit un `.sig` (64 octets) |
| TC-024 | `liora sign verify <module>` vérifie une signature valide |
| TC-025 | `liora sign verify <module>` échoue sur archive modifiée ou signature invalide |
| TC-026 | `liora auth` échange un code d'autorisation (OAuth2 + PKCE) et stocke la session |
| TC-027 | `liora auth` en mode non-interactif sans `LIORIAN_CLI_AUTH_CODE` → erreur catégorisée (exit 2) |
| TC-028 | `liora test` module unique (script `test` → OK) |
| TC-029 | `liora test` tous les modules ; suite en échec → exit 13 |

Scénarios des scripts récents (identifiants préfixés par le numéro du script, `13_` → `15_`) :

| ID | Scénario |
|----|----------|
| 13/TC-030 | `liora marketplace search` liste le catalogue |
| 13/TC-031 | `liora marketplace install` refuse un module non signé (fail-closed) ; `--allow-unsigned` en dev |
| 13/TC-032 | Installation d'un module signé : vérification Ed25519 |
| 14/TC-031 | `liora dev` résout le script `dev` et diffuse la sortie (porte ignorée sans module) |
| 14/TC-031b | `liora dev -p 5010` transmet les flags sans `--` |
| 14/TC-031c | `liora dev -- --port 3000` retire le séparateur `--` |
| 14/TC-032 | `liora check` mappe sur le script `lint` par défaut |
| 14/TC-033 | `liora build` exécute les hooks `before`/`after` autour du script |
| 14/TC-034 | `liora start` résout le script `start` |
| 14/TC-035 | Script absent du `package.json` → échec de résolution (exit 1) |
| 14/TC-036 | Le code de sortie de la commande moteur est propagé (passthrough) |
| 14/TC-037 | La porte de santé (audit) annule `dev` sur module en erreur |
| 15/TC-038 | `liora repair` corrige automatiquement les anomalies bloquantes |
| 15/TC-039 | `liora repair` restitue les points non réparables en instructions (exit non nul) |
| 15/TC-040 | `liora repair --no-interaction` renomme le dossier sur le domaine du manifeste |

---

## 13. Roadmap — Découpage Produit

> **État (2026-09-12)** : les Epics E-001 → E-008 sont largement implémentés dans les releases
> 0.0.8/0.0.9 (branche `alpha`). Le détail de l'alignement spec ↔ roadmap est dans
> `docs/rapport-implementation.md` (§5).

```
Product: liora-cli v1.0.0
│
├── Release 0.1.0 (MVP)
│   │
│   ├── Epic E-001 : Initialisation & Création
│   │   ├── Story S-001 : `liora init` (clone + deps)
│   │   └── Story S-002 : `liora create module`
│   │
│   ├── Epic E-002 : Authentification
│   │   ├── Story S-003 : `liora connect` (email/password)
│   │   ├── Story S-004 : `liora connect` (MFA TOTP)
│   │   └── Story S-005 : `liora disconnect`
│   │
│   └── Epic E-003 : Build & Informations
│       ├── Story S-006 : `liora pack`
│       └── Story S-007 : `liora -v` + `liora help`
│
├── Release 0.2.0 (Store)
│   │
│   ├── Epic E-004 : Publication
│   │   ├── Story S-008 : `liora publish`
│   │   ├── Story S-009 : `liora link`
│   │   └── Story S-010 : `liora unlink`
│   │
│   ├── Epic E-005 : Validation
│   │   ├── Story S-011 : `liora audit`
│   │   └── Story S-012 : `liora debug`
│   │
│   └── Epic E-008 : Signature numérique
│       ├── Story S-019 : `liora sign keygen` (génération clés Ed25519)
│       ├── Story S-020 : `liora sign <module>` (signature canonique .liozip)
│       └── Story S-021 : `liora sign verify <module>` (vérification signature)
│
└── Release 0.3.0 (Qualité)
    │
    ├── Epic E-006 : Expérience développeur
    │   ├── Story S-013 : Mode verbose / logs
    │   ├── Story S-014 : Configuration `lorian.config.json`
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
| R-001 | API `liorian-connect` non disponible | Moyenne | Élevé | Mode offline pour les commandes locales (init, create, pack, audit, debug) |
| R-002 | Incompatibilité keychain sur certaines distributions Linux | Moyenne | Moyen | Fallback transparent fichier chiffré AES-256-GCM (`credentials.enc` / `signing.enc`), clé PBKDF2 du secret machine, `LIORIAN_CLI_STORE` pour forcer le backend |
| R-003 | Taille du binaire trop élevée | Faible | Faible | `ldflags -s -w`, UPX compression optionnelle |
| R-004 | Breaking changes API `liorian-connect` | Faible | Élevé | Versioning API, détection automatique de la version |
| R-005 | Conflits de noms de modules | Moyenne | Moyen | Validation stricte, vérification d'unicité avant création |
| R-006 | Archive `.liozip` corrompue ou falsifiée | Faible | Élevé | Signature canonique Ed25519 (`liora sign`), vérification avant publication (obligatoire) |
| R-007 | MFA bloquant (appareil perdu) | Faible | Élevé | Backup codes, procédure de récupération via `liorian-connect` web |

---

## 15. ADR (Architecture Decision Records)

### ADR-001 : Go + Bubbletea comme stack technique

**Contexte** : Choisir la stack technique pour la CLI Liora.

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
