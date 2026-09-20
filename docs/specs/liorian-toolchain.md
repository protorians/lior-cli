# Lior CLI — Outillage applicatif (`dev`, `build`, `start`, `check`)

> **Statut : CANDIDATE — à implémenter**
>
> Cette spécification décrit l'alignement des commandes du **moteur applicatif du socle**
> (le framework web du template `jetbrains/liorian-socle`) sous la CLI `liorian`. Ces
> commandes restent la passerelle unique vers les scripts d'application déclarés dans le
> `package.json` du projet, afin de permettre, aujourd'hui comme demain, d'exécuter des
> **actions avant et/ou après** la commande du moteur (passerelle extensible).

---

## 1. Contexte & objectifs

### 1.1 Contexte

Le projet initialisé par `liorian init` est une application web dont les scripts
`package.json` (`dev`, `build`, `start`, `lint`, …) délèguent au moteur applicatif.
Aujourd'hui, un développeur lance directement `bun run dev`, `bun run build`, etc. **en
dehors de la CLI** : il n'existe aucun point d'entrée unique permettant d'injecter de la
logique (validation, génération, synchronisation, télémétrie…) autour de ces commandes.

Les commandes `debug` et `test` couvrent déjà le cycle de vie **des modules**
(`library/modules/`) ; il n'existe en revanche **aucune commande d'application** pour
développer, construire, démarrer ou vérifier le socle lui-même.

### 1.2 Objectifs

1. **Alignement** : exposer le cycle d'application sous la CLI (`dev`, `build`, `start`,
   `check`) comme passerelles vers les scripts `package.json` équivalents.
2. **Passerelle extensible** : fournir un point d'injection **avant (`before`) / après
   (`after`)** de la commande du moteur, piloté par la configuration projet, prêt pour
   de futures actions (audit, génération, notifications, déploiement…).
3. **Neutralité** : aucune commande, flag, libellé ou message utilisateur ne révèle le
   nom du moteur applicatif (contrainte de camouflage CI-TOOL-01). Les noms publics sont
   strictement génériques (`dev`, `build`, `start`, `check`).

### 1.3 Valeur ajoutée

- **Un seul point d'entrée** pour tout le cycle de vie (modules + application).
- **Hooks avant/après** sans toucher au moteur ni aux scripts du projet.
- **Sortie temps réel** et arrêt propre de l'arbre de processus (SIGINT → groupe).
- **Passthrough** transparent des arguments supplémentaires vers le script.

---

## 2. Portée

### Dans le périmètre (In Scope)

- `liorian dev` — Passerelle vers le serveur de développement (script `dev`).
- `liorian build` — Passerelle vers la construction de production (script `build`).
- `liorian start` — Passerelle vers le serveur de production (script `start`).
- `liorian check` — Passerelle vers l'analyse statique (script `lint`).
- Hooks `before` / `after` configurables par commande.
- Passthrough d'arguments (séparés par `--`) vers le script.
- Sortie mise en flux, annulation (Ctrl+C / Esc) et codes de sortie prédictibles.

### Hors périmètre (Out of Scope)

- Gestion de la **télémétrie** / diagnostics du moteur (`info`, `telemetry`) — futur.
- Résolution et exécution **directe du binaire** du moteur (`node_modules/.bin/…`) — futur :
  v1 s'appuie exclusivement sur les scripts `package.json`, garantissant la neutralité.
- Lancement avec **timeout** imposé par la CLI — futur (CI responsable du timeout).
- Gestion de plusieurs moteurs/versions (matrice) — hors périmètre produit.
- Les commandes **modules** (`debug`, `test`, `pack`, `audit`) — couvertes par la spec `liorian.md`.

---

## 3. Exigences

### Exigences fonctionnelles

| ID | Description |
|----|-------------|
| TFC-001 | `liorian dev` exécute le script `package.json` `dev` via le gestionnaire de paquets choisi à l'installation (project.packageManager), sinon détecté (bun → pnpm → yarn → npm) |
| TFC-002 | `liorian build` exécute le script `package.json` `build` via le même gestionnaire |
| TFC-003 | `liorian start` exécute le script `package.json` `start` via le même gestionnaire |
| TFC-004 | `liorian check` exécute le script `package.json` `lint` via le même gestionnaire |
| TFC-005 | `liorian dev`/`start` exécute une commande **longue** (serveur) : sans plafond, arrêtée à la sortie du processus ou à l'annulation utilisateur (Ctrl+C / Esc) |
| TFC-006 | `liorian build`/`check` exécute une commande **one-shot** : terminée quand le processus se termine |
| TFC-007 | Les arguments après `--` sont transmis tels quels à la commande (ex. `liorian dev -- --port 3000`) |
| TFC-008 | Le code de sortie de la CLI reflète le code de sortie de la commande sous-jacente (passthrough fidèle) ; l'annulation renvoie `130` |
| TFC-009 | La sortie standard et d'erreur est **mise en flux temps réel** (étape `RUNNING` avec queue de sortie) |
| TFC-010 | Les hooks `before` listés pour la commande s'exécutent (scripts `package.json` via le gestionnaire) **avant** la commande du moteur ; l'échec d'un hook annule la commande |
| TFC-011 | Les hooks `after` listés pour la commande s'exécutent **après** la commande du moteur (même en cas d'échec de celle-ci, sauf annulation) ; l'échec d'un hook rend le run non nulle |
| TFC-012 | `toolchain.commands.<cmd>` permet de surcharger le nom du script `package.json` qui sauvegarde chaque commande (ex. `"check": "typecheck"`) |
| TFC-013 | Si un script requis est absent du `package.json` du projet, la CLI rend une erreur catégorisée avec indice de correction (code de sortie `1`) |
| TFC-014 | Aucun mot-clé du moteur applicatif n'apparaît dans les commandes, flags, aide ou messages (CI-TOOL-01) |
| TFC-015 | `liorian dev` exécute d'abord les contrôles `debug` + `test` sur tous les modules (`library/modules/`), puis le script `dev` ; un contrôle en erreur annule la commande |
| TFC-016 | `liorian build`/`start` exécutent d'abord les contrôles `debug` + `test` + `audit` sur tous les modules, puis le script ; un contrôle en erreur annule la commande |
| TFC-017 | Les contrôles de santé sont ignorés quand le projet ne contient aucun module ; les **warnings** d'un contrôle ne bloquent jamais (seules les **erreurs** annulent) |

### Exigences non-fonctionnelles

| ID | Description |
|----|-------------|
| TNF-01 | Compatible Linux/macOS/Windows, groupes de processus POSIX sur Unix (même moteur que `debug`/`test`) |
| TNF-02 | Sortie bilingue `fr-FR`/`en-US` via les catalogues i18n embarqués |
| TNF-03 | Le `--verbose` et `LIORIAN_CLI_DEBUG` restent fonctionnels (journalisation CLI) |
| TNF-04 | Une commande dont le script n'existe pas ne modifie **aucun fichier** du projet |

---

## 4. Camouflage du moteur (CI-TOOL-01)

Règle stricte : **le nom du moteur applicatif est interdit** dans toute surface utilisateur —
noms de commandes, aliases, libellés, `Short`/`Long`, messages i18n, exemples d'aide, options.

| Surface | Règle | Exemple conforme |
|---------|-------|------------------|
| Noms de commandes | `dev`, `build`, `start`, `check` | `liorian dev` |
| Noms de scripts projet référencés | `dev`, `build`, `start`, `lint` (surchargeables dans la config) | `"check": "lint"` |
| Messages i18n | « moteur applicatif », « outillage », « socle », jamais le nom du moteur | `The application build finished` |
| Code interne | le nom du moteur ne doit pas être injecté dans des chaînes utilisateur | constante interne générique `binaryName` (non utilisée en v1) |

La **v1 n'appelle jamais le binaire du moteur** directement : toute invocation passe par
un script `package.json` (ou, à terme, par `toolchain.binary` — voir §8), ce qui rend le
camouflage structurel et non cosmétique.

---

## 5. Comportement

### 5.1 Correspondance commandes → scripts

| Commande CLI | Script par défaut | Type | Sémantique |
|--------------|-------------------|------|------------|
| `liorian dev` | `dev` | serveur (longue) | Serveur de développement |
| `liorian build` | `build` | one-shot | Construction de production de l'application |
| `liorian start` | `start` | serveur (longue) | Serveur de production |
| `liorian check` | `lint` | one-shot | Analyse statique / vérifications |

> Distinction avec les commandes modules : `dev`/`build`/`start`/`check` opèrent au niveau
> **application** (racine du projet) ; `debug` (build des modules), `pack` (archive `.SenMod`)
> et `test` (suites des modules) restent inchangés.

### 5.2 Résolution de la commande (ordre de priorité)

1. **Gestionnaire de paquets** : `project.packageManager` du `lorian.config.json` s'il est
   disponible sur le PATH, sinon détection `bun → pnpm → yarn → npm`. Aucun gestionnaire →
   erreur catégorisée `Package manager` + indice d'installation (code `1`).
2. **Nom de script** : clé `toolchain.commands.<cmd>` de la config (si présente et non vide),
   sinon le script par défaut du tableau §5.1.
3. **Existence du script** : lecture du `package.json` à la racine du projet. Script présent →
   invocation `<pm> run <script> [-- args…]`. Script absent → **erreur catégorisée** (catégorie
   `Toolchain`) : « no `dev` script in package.json » + indice de correction (code `1`).

La CLI **ne crée, ni ne modifie aucun fichier** lors de la résolution (TNF-04).

### 5.3 Passthrough d'arguments

- Tous les arguments positionnels passés à la commande sont transmis au script.
- Séparateur `--` : `liorian build -- --no-lint` → `<pm> run build [--] --no-lint`.
- Pour `npm`, un séparateur `--` est interposé avant les arguments (convention npm) ;
  `bun`, `pnpm`, `yarn` reçoivent les arguments tels quels.

### 5.4 Exécution & rendu

Le rendu suit le vocabulaire d'étapes de `debug`/`test` :

| Étape | ID | Statuts | Détail |
|-------|----|---------|--------|
| Résolution | (sans ID) | `SUCCESS` \| `ERROR` | commande résolue (`bun run dev`) ou échec résolution |
| Hook `before` | `before:<cmd>:<n>` | `RUNNING` → `SUCCESS` \| `ERROR` | `<pm> run <hook>` |
| Commande moteur | `cmd:<name>` | `RUNNING` → `SUCCESS` \| `ERROR` | sortie en flux (queue de 8 lignes), détail final |
| Hook `after` | `after:<cmd>:<n>` | `RUNNING` → `SUCCESS` \| `ERROR` | `<pm> run <hook>` |

- Les **serveurs** (`dev`, `start`) restent en `RUNNING` jusqu'à la sortie du processus ou
  l'annulation ; la sortie s'affiche en continu.
- Les **one-shot** (`build`, `check`) passent à `SUCCESS`/`ERROR` à la terminaison.
- **Annulation** : `Ctrl+C` / `Esc` → arrêt de l'arbre de processus (commutateur
  `runner.Run`), rendu `⊘`, retour `tui.ErrCancelled` → code de sortie `130`.

### 5.5 Hooks `before` / `after`

Les hooks sont **des noms de scripts `package.json`** exécutés via le gestionnaire de paquets
(`<pm> run <hook>`). Ils sont déclarés dans la configuration projet (§6).

- **`before`** : exécutés séquentiellement avant la commande moteur. Le premier échec
  **abandonne** la commande (le code de sortie du hook est propagé, étape `ERROR`).
- **`after`** : exécutés séquentiellement après la commande moteur, y compris en cas
  d'échec de celle-ci, mais **pas en cas d'annulation**. Le premier échec rend le run
  non nul (code du hook propagé, étape `ERROR`) ; les hooks suivants ne sont pas exécutés.

Ordre global : `before` → commande moteur → `after`.

### 5.6 Codes de sortie

| Situation | Code |
|-----------|------|
| Succès (commande et hooks) | `0` |
| Échec de résolution (gestionnaire / script absent) | `1` (erreur catégorisée) |
| Échec de la commande moteur | code de sortie du processus (passthrough) |
| Échec d'un hook | code de sortie du hook |
| Échec d'un contrôle de santé des modules (TFC-015/-016) | code du contrôle (debug `10`, test `13`, audit `1`) |
| Annulation utilisateur | `130` |

### 5.7 Porte de santé des modules (pre-flight)

Avant de proxier un cycle d'application, la CLI **garantit que les modules sont sains** :
les modules sont vérifiés une première fois, et la commande est abandonnée au premier
contrôle en erreur (TFC-015/-016, TFC-017).

| Commande | Contrôles exécutés avant |
|----------|--------------------------|
| `liorian dev` | `debug` + `test` |
| `liorian build` | `debug` + `test` + `audit` |
| `liorian start` | `debug` + `test` + `audit` |
| `liorian check` | aucun |

Comportement :

- Les contrôles reprennent la **sémantique des commandes modules** : une étape en `ERROR`
  (debug, test) bloque ; pour `audit`, seules les **erreurs** (`TotalErrors() > 0`) bloquent.
  Les **warnings** (`WARNING`, `SKIPPED` — modules sans tests, sans script de build, sans
  gestionnaire) n'annulent pas la commande.
- Un projet **sans module** (`library/modules/` absent ou vide) est exempté de contrôle :
  il n'y a rien à vérifier, la commande s'exécute normalement.
- La porte est **non interactive** : elle n'ouvre aucune sélection de package de test
  (`prepareRunners`), contrairement à `liorian test` lancé seul ; un module sans runner
  configuré est simplement sautée en `WARNING`.
- Un contrôle en erreur affiche ses détails (logs des modules en erreur pour `debug`/`test`,
  tableau d'audit pour `audit`), puis la CLI renvoie une erreur catégorisée `Toolchain` avec
  indice menant vers la commande standalone (`liorian debug`, `liorian test`, `liorian audit`).
- L'annulation (Ctrl+C / Esc) pendant un contrôle renvoie `130` (même flux que la commande).

> Les éventuels hooks `before` de la commande (`toolchain.before`) s'exécutent **après** la
> porte de santé, juste avant le script du moteur.

> La CLI **propage** le code de sortie de l'outil sous-jacent (passthrough fidèle, TFC-008)
> au lieu de le normaliser, pour rester transparente vis-à-vis des chaînes CI/CD.

---

## 6. Configuration projet (`lorian.config.json`)

Nouvelle section optionnelle `toolchain` (sauvegardée telle quelle, aucune réécriture) :

```json
{
  "project": { "name": "mon-projet", "packageManager": "bun" },
  "toolchain": {
    "commands": {
      "check": "typecheck"
    },
    "before": {
      "build": ["audit-local", "generate-api"]
    },
    "after": {
      "start": ["open-dashboard"]
    }
  }
}
```

| Clé | Type | Rôle |
|-----|------|------|
| `toolchain.commands` | `map<string,string>` | Sur -charge du nom de script par commande liorian (`dev`, `build`, `start`, `check`) |
| `toolchain.before` | `map<string,string[]>` | Scripts `package.json` lancés avant la commande (clés : mêmes noms de commandes liorian) |
| `toolchain.after` | `map<string,string[]>` | Scripts `package.json` lancés après la commande |

Toute clé inconnue d'une autre section est ignorée (structure Go `omitempty`, decodage strict
impossible sans changer le format existant).

---

## 7. Téléchargement/messages i18n & affichage

Nouvel ensemble de clés (catalogues `fr-FR`/`en-US`) :

| Clé | fr-FR | en-US |
|-----|-------|-------|
| `cmd.dev.short` | « Démarrer le serveur de développement » | « Start the development server » |
| `cmd.build.short` | « Construire l'application » | « Build the application » |
| `cmd.start.short` | « Démarrer l'application (production) » | « Start the application (production) » |
| `cmd.check.short` | « Exécuter l'analyse statique » | « Run static checks » |
| `cat.toolchain` | « Outillage » | « Toolchain » |
| `toolchain.step.resolve` | « Résolution de la commande » | « Command resolution » |
| `toolchain.step.before` | « Actions préalables » | « Pre-actions » |
| `toolchain.step.after` | « Actions finales » | « Post-actions » |
| `toolchain.step.run.dev` | « Serveur de développement » | « Development server » |
| `toolchain.step.run.build` | « Construction » | « Build » |
| `toolchain.step.run.start` | « Serveur de production » | « Production server » |
| `toolchain.step.run.check` | « Analyse statique » | « Static checks » |
| `toolchain.step.run.failed` | « La commande a échoué (code %d) » | « Command failed (exit %d) » |
| `toolchain.cancelled` | « Commande interrompue » | « Command interrupted » |
| `toolchain.error.pm_none` | « Aucun gestionnaire de paquets disponible » (+ fix) | « No package manager found » (+ fix) |
| `toolchain.error.no_script` | « Aucun script « %s » dans package.json » (+ fix) | « No "%s" script in package.json » (+ fix) |
| `gate.spinner.debug` | « Contrôle des modules (debug) » | « Checking modules for build errors (debug) » |
| `gate.spinner.test` | « Tests des modules (test) » | « Running module tests (test) » |
| `gate.spinner.audit` | « Audit de conformité des modules (audit) » | « Checking module conformance (audit) » |
| `gate.failed.debug/test/audit` | « le contrôle … a échoué : %d … » | « module … check failed: %d … » |
| `gate.failed.fix` | « Corrigez … ou exécutez `liorian %s` » | « fix …, or run `liorian %s` » |
| `gate.cancelled` | « Contrôles des modules interrompus » | « Module checks interrupted » |

---

## 8. Périmètre futur

- **Diagnostics `info` / télémétrie** : passerelle `liorian info` (script `info` absent de
  base → nécessite `toolchain.commands.info`). Non implémenté en v1.
- **Exécution directe du binaire** : résolution `node_modules/.bin/<tool>` puis PATH lorsque
  aucun script n'existe — à sécuriser vis-à-vis de CI-TOOL-01 (détail résolu via un libellé
  neutre).
- **Timeout CLI** : option `--timeout` pour les one-shot (proxy) — la CI reste responsable
  du timeout en v1.
- **Hooks liorian-complets** : exécuter des sous-commandes liorian (`liorian audit`) en hook
  sans passer par un script `package.json` (exécuteur de ligne de commande portant le shell).
- **Passerelle générique** : `liorian run <script>` à liste blanche de scripts d'application.

---

## 9. Tests & Definition of Done

### Tests unitaires (`internal/toolchain`)

- Résolution : mapping défauts (`dev→dev`, `check→lint`), surcharge `toolchain.commands`,
  script absent → erreur, gestionnaire absent → erreur.
- Construction des arguments par gestionnaire : `npm` reçoit `--` avant les extra, `bun` non.
- Hooks : ordre `before → cmd → after`, échec `before` annule, `after` exécuté après échec
  de la commande mais pas après annulation, code de sortie propagé.
- Complexité passthrough : arguments `--` transmis.
- Porte de santé : `dev` demande `debug`+`test`, `build`/`start` demandent `debug`+`test`+`audit`,
  sans module le contrôle est ignoré, un module sain passe, un module en erreur (validation /
  test / dépendance d'audit) annule avec le code du contrôle, `check` n'est jamais contrôlé.

### Tests E2E (testscript)

- Nouveau script `08_toolchain.txtar` : `dev`, `build`, `check`, `start` résolus par le
  gestionnaire « bun » (fixture), sortie attendue, hooks `before`/`after` exécutés, code de
  sortie propagé pour un script absent.
- Fixtures `bin/bun` & `bin/npm` ouverts aux scripts `start` et `lint`.

### DoD

- [ ] `gofmt -l .` propre · `go vet ./...` propre · `go test ./...` vert · `go test ./e2e/ -run TestScripts` vert.
- [ ] Aucun mot-clé du moteur dans les surfaces utilisateur (`rg` sur `cmd/`, `internal/toolchain/`, i18n).
- [ ] `README.md` (table des commandes) et `CHANGELOG.md` documentent les 4 commandes.
- [ ] Spec actuelle (`docs/specs/liorian.md`) référence `docs/specs/liorian-toolchain.md`.