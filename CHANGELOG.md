# Changelog

All notable changes to this project will be documented in this file.


## [Unreleased]

## [v0.33.0] - 2026-10-01

### Added
- **Scénario E2E `19_create_session.txtar`** — la création pilotée par la session est couverte de bout
  en bout contre le mock `liorian-connect` : le handler in-memory
  `GET /api/developer-store/accounts/me` sert désormais le compte développeur de l'email authentifié
  (`connected@example.com` possède le slug `acme`, les autres comptes n'en exposent aucun — les
  scénarios existants se dégradent donc exactement comme avant). Le scénario vérifie le récapitulatif
  de `connect` (slug affiché), la composition du domaine canonique `mod.acme.crm` sans `--domain` ni
  `--publisher`, et le refus d'une organisation **sans slug** avec le message qui renvoie à la
  définition interactive (TC-046 / TC-047).

### Changed
- **Documentation du flux `create module` piloté par la session** — la spécification décrit
  désormais l'implémentation réelle plutôt que l'ancien ordre de prompts :
  - **§3 / FR-005b, FR-008b** — les exigences sont scindées (identité composée depuis la session
    pour `create module`, résolution + affichage du slug pour `connect`) ;
  - **§5.2 « Comportement »** — les trois étapes réelles (type → slug → domaine pré-rempli) sont
    documentées avec la hiérarchie des sources (`--publisher` > slug connecté > définition forcée),
    la réécriture de préfixe par `CanonicalizeDomain`, et la composition non interactive ;
  - **§5.2 « Contraintes »** — grammaire `canonicalDomainRE` (six préfixes, trois labels),
    refus sans session (`ExitAuth`) et store injoignable distingué de l'absence de session
    (`ExitNetwork`), valeur normalisée du slug ;
  - **§5.19** — la section est réécrite autour de la chaîne native : tableau des actions, tableau des
    flags par action (`--port`/`LIORIAN_DEV_PORT`, `--host`, `--https`/`--http`, `--strict-port`,
    `--socle`, `--out`, `--no-build`), résolution de cible, HMR SSE, port/TLS, codes de sortie,
    `bind:socle`/`unbind:socle` ;
  - **§5.2 « Sortie TUI »** — l'aperçu du wizard (menu des types avec préfixe, curseur en fin de
    saisie du domaine, identifiant déduit du dernier label) et un second aperçu pour
    l'**organisation sans slug**, où le `409` est expliqué comme réutilisation et non comme échec ;
  - **§5.3 `connect`** — nouvelle étape de résolution du slug et récapitulatif qui affiche le
    slug **même vide**, avec la raison de ce choix ;
  - **§5.10** — la règle d'audit « domaine canonique » est distincte du reverse-DNS générique dans
    le tableau des règles (elle existe bien dans `validator.go`) ;
  - **§12** — TC-003/TC-004 reformulés en cas de domaine (le test porte sur le domaine, plus sur
    un « nom ») ;
  - **README** — ligne `create module` (flags `--id` / `--publisher`, enchaînement du wizard) et
    ligne `connect` (slug affiché, définition proposée) ;
  - **`docs/rapport-implementation.md`** — sections `create module` et `connect` alignées sur
    `store.ResolveOrganizationSlug` / `store.RegisterOrganizationSlug` / `module.CanonicalizeDomain`.
- **Documentation alignée sur la chaîne `artifact` native** — le README, `docs/specs/liora.md`
  (§4.1 arbre `internal/artifactdev|artifactbind|devlink`, §5.19, §5.11 ligne d'aide `artifact`),
  `docs/specs/liora-toolchain.md` et
  `docs/rapport-implementation.md` décrivent la chaîne portée par le binaire ; l'ADR-003 est marqué
  **révisé** (l'option C — passthrough vers `@liorian/artifact-kit` — est explicitement abandonnée et
  remplacée par le portage natif, avec ses justifications). Le commentaire d'en-tête de
  `internal/socle` ne renvoie plus à un package npm supprimé mais à `internal/artifactbind`, la seule
  implémentation du contrat de liaison.

### Fixed
- **`gofmt` sur `internal/store/signing_key_test.go`** — ligne vide superflue (restée de v0.24.0) ;
  `gofmt -l` ne rapporte plus aucun fichier.

## [v0.32.0] - 2026-10-01

### Added
- **Création de module pilotée par la session (`liora connect`)** — le wizard
  `liora create module` construit l'identité en trois étapes : (1) la **session est
  obligatoire** — le slug de l'organisation connectée (`GET /developer-store/accounts/me`)
  ancre l'identité, et une organisation **sans slug est obligée de le définir via la CLI**
  (`POST /developer-store/accounts/register`, normalisation kebab-case, repli sur le slug
  déjà défini en cas de conflit) ; le même contrôle renforce `liora connect`, dont le
  récapitulatif affiche désormais le slug d'organisation ; (2) le **type de module est
  demandé** parmi les types de distribution supportés (`tui.Select` — il décide du préfixe
  canonique du domaine) ; (3) le **domaine inversé est proposé pré-rempli**
  `<prefixe(type)>.<slug-organisation>.` — le développeur ne complète que l'identifiant
  (`tui.AskTextPrefilled`, curseur en fin de saisie) et l'identité entière est conservée
  pour la suite du processus. `--publisher` devient l'échappatoire CI / rejeu de script ;
  la question interactive du publisher est retirée.
- **`liora artifact` devient la chaîne de développement native du module** — `build`, `dev`, `pack`,
  `typecheck`, `test`, `bind:socle` et `unbind:socle` vivent désormais dans le binaire `liora`
  (`internal/artifactdev` + `internal/artifactbind`), avec esbuild intégré via son **API Go** : le
  bundle, le document hôte templatisé (`{{manifest.*}}`), les alias first-party (`@liorian/sdk`,
  `@liorian/module-*` → sources), le wrapper d'amorçage et la validation non-vide sont portés à
  l'identique. Le dev-server sert `.liorian/artifact/` (D7) avec `Access-Control-Allow-Origin: *`,
  un repli SPA et un flux **SSE** `/-/events` qui recharge les iframes après chaque rebuild — le
  HMR reste un rechargement de document, piloté par un watcher maison (sondage léger de l'arbre du
  module, sources + template + manifeste + config) qui déclenche le rebuild incrémental du contexte
  esbuild (l'API Go de esbuild v0.28 n'expose pas de callback de watch). La résolution TLS mkcert
  conserve son ordre (env explicite → socle lié → `~/.config/liorian/certs` → module) et son échec
  fermé. Aucune dépendance Node n'est requise dans le module pour la toolchain.
- **Gestion du port du dev-server (EADDRINUSE)** — le port demandé (5178 par défaut) est sondé
  avant le service et, s'il est occupé (dev-server orphelin qui survit à un terminal fermé sans
  SIGINT, docker-proxy), le serveur **bascule sur le port suivant** avec un message explicite,
  réaligne `.liorian/dev.json` et rappelle de re-binder/redémarrer le socle pour que
  `NEXT_PUBLIC_DEV_MODULES_URL` suive. `--strict-port` refuse au lieu de basculer. Sur `localhost`,
  le serveur écoute sur **les deux piles de loopback** (IPv4 + IPv6) : un navigateur qui résout
  `localhost` vers `::1` (macOS le fait en premier) n'est plus dépendant d'un bind mono-pile.
- **Orchestration un-terminal — `liora artifact dev --socle <dir>`** : démarre aussi le socle
  (script `dev` : next dev + serveur de bibliothèque) dans son propre groupe de processus et le
  stoppe avec le dev-server (`Ctrl+C`). La boucle complète — socle, bibliothèque, dev-server HMR —
  tient dans un seul terminal.
- **`liora artifact pack`** : build + validations §4.4 (`Validator.ValidateModuleDir`, D16 compris)
  + archive `.LiorArtifactPackage` dans `.liorian/build/` (ou `--out`), sans signature —
  l'itération locale ; `liora pack` reste le cycle signé avant publication. Le packer gagne
  `PackPath` (module désigné par son répertoire) et `RunTypecheck` ignore désormais un script
  `typecheck` du module qui ré-invoque la CLI (`liora artifact typecheck`) — la délégation
  créait une boucle infinie ; `tsc --noEmit` est exécuté en direct.
- **Domaine canonique du module par type** (`docs/modules/module-manifest.md` §6.2) — le préfixe
  du domaine dépend du type de distribution : `config`, `system`, `service`, `widget`, `theme`,
  sinon `mod` pour les applications web. `liora create module` compose le domaine canonique
  `<prefixe(type)>.<slug-editeur>.<identifiant>` quand `--domain` est omis (slug éditeur fourni
  par la session authentifiée — `store.ResolveOrganizationSlug` — ou l'échappatoire
  `--publisher`), déduit l'identifiant du **dernier label** du domaine
  (`mod.liorian.accounting` → `accounting`), et **réécrit le préfixe** d'un domaine non canonique
  fourni (`com.acme.billing` + WEB_APP_LOCAL → `mod.acme.billing`, avertissement explicite). Un
  domaine de plus ou moins de trois labels est refusé. La grammaire `canonicalDomainRE` accepte
  les six préfixes.
- **Politique d'assets et d'exécutables au pack (fail-closed)** — deux niveaux sur toutes les
  entrées (`src/**`, `artifact/**`, layout legacy) : (1) **exécutables refusés**, par extension
  (`.exe`, `.sh`, `.bash`, `.zsh`, `.ps1`, `.py`, `.rb`, `.php`, `.jar`, `.msi`, `.deb`, `.apk`,
  `.node`, `.so`, `.dylib`, `.dll`, `.o`, `.obj`…) **et par contenu** — un binaire ELF, Mach-O,
  PE, *fat binary*, un shebang ou un ZIP imbriqué est refusé quelle que soit son extension ; (2)
  **allowlist d'assets** : code/config (`ts`, `tsx`, `js`, `css`, `html`, `json`, `svg`, `md`,
  `yaml`, `csv`, `wasm`…), images (`png`, `jpg`, `webp`, `avif`, `gif`, `bmp`, `ico`), vidéos
  (`mp4`, `webm`, `mov`, `m4v`), audios (`mp3`, `wav`, `ogg`, `m4a`, `aac`, `flac`), polices
  (`woff`, `woff2`, `ttf`, `otf`, `eot`) — tout autre type est refusé avec la liste des
  catégories admises, et un fichier sans extension ne voyage que s'il est du texte. Le packer
  n'archive enfin plus les fichiers et dossiers cachés (`.env`, `.gitignore`, `.DS_Store`) que le
  socle refuse de toute façon à l'installation. Miroir côté socle : `ALLOWED_SERVED_EXTENSIONS`
  (`@liorian/extended-kit`) couvre les mêmes médias et l'audit d'archive refuse bloquante les
  exécutables à la publication et à l'installation.

### Changed
- **Le passthrough `@liorian/artifact-kit` est retiré** — `internal/artifactkit` (Ensure/Resolve/
  Exec) et son binaire Node sont supprimés ; `liora artifact <action>` n'installe plus rien dans
  le module et n'a plus besoin de `node_modules`. `liora doctor --fix` exécute la **même**
  implémentation native de `bind:socle` (`artifactbind.Bind`) au lieu de déléguer au binaire du
  module : le contrat de liaison ne peut plus diverger entre le diagnostic et la commande. Le
  portage de la liaison reprend les garanties du kit : symlinks (repli copie), pointeur `current`
  restauré, marqueur `.liorian-bind.json` (une installation réelle n'est jamais touchée),
  alignement de schéma, préservation des hôtes personnalisés (`host.docker.internal`), ajout/retrait
  idempotent dans `NEXT_PUBLIC_DEV_MODULES`.

### Removed
- **Package npm `@liorian/artifact-kit` (côté workspace Liora)** — la source de vérité de la
  chaîne de build est désormais la CLI. Les scripts `build:modules` / `pack:modules` du workspace
  délèguent à `liora artifact build|pack` ; un module n'a plus à déclarer la dépendance ni le
  binaire `artifact` dans ses scripts.

## [v0.31.0] - 2026-10-01

### Added
- **`create module --standalone` — un module dans son propre dépôt, sans cloner le socle** — la
  forme normale d'un module tiers est un dépôt git public contenant un seul module, que n'importe qui
  peut cloner et construire. La CLI ne pouvait pourtant le produire qu'en exigeant un projet déjà
  initialisé : le développeur devait cloner le socle (`init`), y créer son module, puis extraire le
  dossier — un détour qui n'a rien à voir avec son travail, et dont le résultat était un dépôt
  porteur du `.env` et des certificats du socle. `create module --standalone` fait de la racine du
  répertoire courant la racine du dépôt : `liorian.config.json` minimal (`private: true`,
  `module: "modules"`), `.gitignore` dédié excluant `.lierian/` — le lien de développement décrit
  une machine, chemin absolu —, certificats et archives, et un `README.md` écrit en entier parce
  que la boucle à trois terminaux est la partie qu'un gabarit ne peut pas deviner. Le module est
  écrit dans `modules/<id>/`, **la disposition exacte d'un module de première partie** : `pack`,
  `sign`, `publish`, `artifact …` fonctionnent donc sans cas particulier.
  Le dépôt est **strictement vidé** : un répertoire qui contient autre chose que ce que la commande
  écrit (ou `.git` / `.github` / un `modules/` vide) est refusé avec sa marche à suivre, parce
  qu'un dépôt de module a une forme dont `pack`, `publish` et `doctor` dépendent — un scaffold
  silencieux à côté d'une application existante les casserait de façon difficile à remonter à sa
  cause. Un dépôt autonome imbriqué dans un autre projet est refusé pour la même raison : la
  remontée depuis `modules/<id>/` s'arrêterait à la racine la plus proche et le développeur
  publierait depuis le mauvais arbre.
- **Aucun `src/app/` dans un dépôt autonome** — la page `src/app/<url>/page.tsx` est une **route
  du socle**, pas du module : le socle la monte sous `src/app/` et sert le module isolé sur
  `/m/<id>`. Un module développé dans son dépôt n'a pas de route à lui, et l'arbre produit ne serait
  construit par rien. `module.Creator.NoPage` retire ce scaffolding, et l'`uri` du manifeste reçoit
  par défaut le préfixe `/m/<id>` (`config.ModuleRoutePrefix`) — cette valeur est l'adresse que le
  résolveur de routes du socle lit, donc une adresse sans le préfixe serait déclarée et jamais
  montée.
- **`liora doctor` — nommer la cause quand la boucle de développement ne produit aucune erreur** —
  le symptôme le plus fréquent du modèle isolé — « Module introuvable ou non installé » alors que le
  module est lié, une page blanche alors que le socle fonctionne — ne laisse **rien** dans les
  journaux : un registre d'installation vide sous `next dev`, un schéma d'URL divergent que le
  navigateur refuse en *mixed content* sans message, un certificat absent, un socle arrêté. La
  commande rend ces états visibles sous forme de contrôles nommés (`ok` / `warn` / `fail` /
  `skipped`), chacun avec son détail et **la commande à exécuter pour le lever** : identité du socle
  et de son schéma, `.env`, certificats mkcert, racine de transport de la bibliothèque
  (`NEXT_PUBLIC_LIBRARY_MODULES_URL`), **réponse effective** de l'index d'installation — la preuve,
  là où les variables ne sont qu'une déclaration —, `manifest.id`, liaison `.lierian/dev.json`
  (stale ou vers un autre socle), transport du dev-server, et socle en écoute. Le socle est localisé
  par `--socle`, puis par la liaison du module, puis par recherche ascendante, puis parmi les dépôts
  voisins ; un module absent du périmètre n'est pas une erreur, le socle se diagnostique seul.
- **`doctor --fix` — corriger sans risque ce qui peut l'être** : crée le `.env` du socle depuis son
  gabarit (secrets synthétisés : clé applicative, paire VAPID), câble la racine de transport de la
  bibliothèque dans `.env.local`, provisionne les certificats quand `mkcert` est disponible, et
  **re-lie le module via `artifact bind:socle`** — le même chemin de code que la commande manuelle,
  dont la liaison corrige d'un geste ce qui la précède : installation dans la bibliothèque, identité
  de transport et URL du dev-server. Une correction qui échoue reste lisible (l'erreur est
  affichée sur la ligne) au lieu de rendre le rapport identique à ce qu'il était avant la tentative.
  Une valeur déclarée mais divergente du port détecté n'est jamais réécrite : elle peut venir d'un
  proxy, et un diagnostic qui devine se trompe.
- **`doctor --output json` — un module dont la boucle est cassée doit pouvoir faire échouer une CI** :
  le rapport sérialise le profil du socle, le module visé, chaque contrôle et les corrections
  appliquées ; la commande sort en erreur dès qu'un contrôle est en échec, là où `table` se contente
  de colorer un rapport. Les certificats auto-signés de `mkcert` sont acceptés par la sonde : la
  question posée est l'atteignabilité, le navigateur valide lui-même la chaîne.

### Technical Details
- **`internal/socle`** — nouveau package décrivant le dépôt d'un socle tel que la CLI le voit :
  topologie (schéma, ports, serveur de bibliothèque), état de développement (`.env`, certificats)
  et contrat de liaison. Le contrat est volontairement identique à celui implémenté par
  `@liorian/artifact-kit` (`src/socle.ts`, `src/dev-link.ts`) : les deux lectures portent sur les
  mêmes fichiers, sans dépendance d'exécution entre les deux outils. `ReadProfile` ne échoue
  jamais — un socle inhabituel produit un profil partiel, parce que le diagnostic doit pouvoir
  s'afficher quand rien ne va. `IsSocle` retient `library/`, `serve.mjs` ou un `package.json#name`
  contenant « socle », **jamais le nom du dossier** : un checkout mal nommé n'est pas un socle.
  L'index d'installation est lu comme ce qu'il est — un **listing de répertoires** —
  (`[{ "name": "accounting", "type": "directory" }]`, `serve.mjs` / `createLibraryHandler`), dont les
  entrées non-`directory` sont ignorées comme le fait `hydrateFromLocalLibrary`. 14 tests.
- **`Creator.NoPage`** n'est qu'un interrupteur : `create` dans un projet continue de scaffolder la
  page, seul le dépôt autonome l'omet. `createRoot()` isole la différence — sans `--standalone`, la
  résolution de racine de projet est inchangée.
- **Le dépôt autonome est marqué `private: true`** dans sa `liorian.config.json` : un module se
  publie explicitement (`liora publish`), jamais par le seul fait d'être présent.

## [v0.30.0] - 2026-09-30

### Changed
- **Le code projet redevient `liorian` sur tous les chemins que la CLI écrit** — `v0.13.0` avait
  raccourci la couche « code de projet » de `liorian` à `lorian` (fichiers de configuration, dossier
  projet, vault utilisateur, services keychain) alors que le reste de l'écosystème — plateforme
  `Liorian`, domaines `*.liorian.protorians.com`, SDK `@liorian/sdk`, variables `LIORIAN_*` — n'avait
  jamais suivi. Le CLI se retrouvait avec une graphie qui ne correspondait à aucun de ses propres
  livrables ; elle redevient `liorian`, ce qui homogénéise `liorian.config.json` / `liorian.config.toml`,
  le dossier projet `.lierian/` (dont `.lierian/build/`), le vault `~/.lierian-cli` et les services
  keychain `liorian-cli` / `liorian-cli-signing` avec le reste de la marque. Le schéma du registre
  d'applications est renommé à l'identique (`liorian.config.schema.json`), sans quoi le pointeur
  `$schema` de `app.config.json` visait un fichier inexistant et l'éditeur perdait la complétion.
- **La graphie est alignée partout où l'utilisateur la lit** — aide et exemples des commandes,
  catalogues i18n `en-US` / `fr-FR`, `README.md`, specs `docs/specs/liora.md` et
  `docs/specs/liora-toolchain.md`, et scénarios E2E. Le `.gitignore` suit : il ignorait encore
  `.lorian/` alors que le CLI écrivait `.lierian/`, donc toutes les archives produites depuis
  `v0.13.0` se retrouvaient versionnées.
- **Rupture** — un projet existant doit renommer `lorian.config.{json,toml}` en `liorian.config.*` et
  `.lorian/` en `.lierian/` (`.lorian/build/` → `.lierian/build/`) ; les archives déjà packées sous
  l'ancien nom restent lisibles, il faut seulement les déplacer ou les reconstruire. Le vault local et
  le keychain sont renommés eux aussi : `~/.lorian-cli` et les services `liorian-cli` /
  `liorian-cli-signing` deviennent illisibles, il faut se ré-authentifier (`liora connect`) et
  re-générer les clés de signature (`liora sign keygen`). Bump `v0.30.0` (ligne 0.x).

### Technical Details
- **Entrée `lorian` retirée des répertoires exclus du scan de module** — le renommage en laissait
  une seconde clé dans `excludedSourceDirs`, devenue redondante.

## [v0.29.0] - 2026-09-30

### Added
- **Section `manifest.legal` — les documents qu'un utilisateur doit accepter avant d'utiliser un
  module** — un module peut désormais déclarer dans son `manifest.json` les documents légaux qui
  conditionnent son usage (conditions d'utilisation, politique de confidentialité, et — au choix —
  un contrat de licence). `pack` refuse une déclaration incomplète, là où le serveur aurait rejeté
  la publication **et verrouillé son premier utilisateur dehors** (D16, spec
  `module-isolated-runtime.md` §6.11). La section reste **facultative** : un module qui ne la
  déclare n'a ni obligation ni porte à l'usage — l'état de tous les modules existants — et le coût
  de l'absence est nul. Dès qu'un document est déclaré, `TERMS` et `PRIVACY` sont tous deux
  obligatoires, parce qu'un module qui livre des CGU sans politique de confidentialité est
  exactement le cas que cette porte d'acceptation existe pour empêcher ; `LICENSE` est la seule
  catégorie que `required: false` admet.
  Le contenu reste en `json.RawMessage` et la CLI n'en valide que l'**enveloppe** : clé kebab-case,
  catégorie connue, `version` en SemVer — indépendante de la version du module, sans quoi deux
  révisions des mêmes CGU seraient indistinguables et accepter l'une accepterait l'autre — et corps
  porteur de texte. Les trois notations (tableau de sections, objet de sections, Markdown) sont
  acceptées et interchangeables : le serveur normalise et checksumme la forme canonique, donc
  réécrire ses conditions d'une notation à l'autre ne force pas une nouvelle acceptation. La
  normalisation et l'empreinte restent côté serveur (`@liorian/api-resources/module-legal.util`) :
  une seconde implémentation ici dériverait librement de celle qui décide, et le `Marshal` demeure
  byte-exact à la réémission.

## [v0.28.0] - 2026-09-30

### Added
- **Commande `liora artifact <action>` — passthrough vers `@liorian/artifact-kit`** — la chaîne de build
  d'un module (validation, bundle, doc hôte, archive) appartient à `@liorian/artifact-kit`, pas à la
  CLI Go : `liora artifact` la **transmet** au binaire `node_modules/.bin/artifact` du module visé,
  action et options `--port`/`--out`/`--host`… **telles quelles** (`DisableFlagParsing`, donc aucune
  option n'est réinterprétée par Cobra et le contrat de la CLI `artifact` reste le sien). Le module
  est celui qui porte le répertoire courant, à défaut celui nommé par les arguments, à défaut celui
  sélectionné depuis la racine du projet — le répertoire n'est ajouté en dernier argument que s'il
  diffère du répertoire courant, si bien que `liora artifact build` et
  `cd modules/blog-manager && liora artifact build` visent la même cible. `stdin`/`stdout`/
  `stderr` sont reliés au terminal et le processus enfant n'est pas isolé dans son groupe : `Ctrl+C`
  atteint toute la hiérarchie, `artifact dev` reste interruptible. Le code de sortie de la CLI est
  celui de `liora artifact`. Si `@liorian/artifact-kit` manque du `package.json` du module, il est
  installé comme dépendance de runtime avec le gestionnaire du projet avant l'exécution — et la
  commande **échoue** si le gestionnaire se déclare réussi sans que la dépendance apparaisse, plutôt
  que de laisser croire à une CLI disponible qui ne l'est pas. ADR-003 (`docs/specs/liora.md` §5.19).
- **Actions `liora artifact bind:socle` / `unbind:socle` — liaison d'un module à un socle hors de
  son dossier** — `artifact` reste un passthrough, mais ces deux actions prennent le **dossier du
  socle** en premier argument : `liora` l'absolutise contre le répertoire d'appel, résout le module
  en cours (répertoire courant, argument, ou sélection à la racine), puis transmet l'action au
  binaire `artifact` exécuté dans le module. Côté `@liorian/artifact-kit`, la liaison écrit
  `<socle>/library/modules/<id>/` (pointeur `current`, `<version>/manifest.json` et
  `<version>/artifact`) en **liens symboliques** vers le module (repli copie), et câble le HMR dans
  le `.env.local` **non versionné** du socle (`NEXT_PUBLIC_DEV_MODULES_URL`,
  `NEXT_PUBLIC_DEV_MODULES`) — le dépôt du socle reste intact. `unbind:socle` retire la liaison via
  son marqueur (`.liorian-bind.json`) sans jamais supprimer une installation réelle. Le module peut
  donc vivre dans `modules/<id>/` du workspace et se pousser sur un dépôt public.
- **Commande `liora sign trust`** — exporter la clé publique du trousseau de signature pour l'épinglage du socle : `liora sign trust
  [--format env|json|pem|keyid] [--merge <fichier>]` écrit la clé locale au format demandé —
  `NEXT_PUBLIC_MODULE_TRUST_KEYS` prêt à coller (env), le trousseau `keyId → PEM SPKI` (json), la clé
  seule (pem), ou son `keyId` seul pour un CI (keyid). `--merge` agrège un trousseau existant sans
  l'écraser, afin de ne pas perdre les clés déjà publiées. Sans trousseau, la commande échoue avec un
  diagnostic au lieu de rendre un trousseau vide — indiscernable d'une absence de contrôle. Sortie
  déterministe, rejouable en CI.

### Changed
- **Le format d'artefact s'appelle désormais `.LiorArtifactPackage` (rupture)** — ZIP renommé,
  casse exacte avec un `L` majuscule, aligné sur le nom de la commande `artifact` qui le produit et
  sur `config.ArchiveFormatLabel = "Lior Artifact Package"` pour les messages. La comparaison est
  **sensible à la casse** : l'ancien `.liozip`, comme les extensions historiques `.SenMod` et `.smp`,
  ne sont plus reconnus nulle part. `config.LegacyArchiveExts` est supprimé et la recherche de
  signature (`signing.FindArchive`) ne retient plus qu'un candidat. Toutes les archives déjà
  construites doivent être régénérées (`artifact pack` ou `liora pack`) avant `sign`, `publish`,
  `install` ou `marketplace install` : une archive à l'ancienne extension est **introuvable**, pas
  seulement non signée. `docs/specs/liora.md` (FR-035, §5.5), `README.md` et
  `docs/plan-mise-a-niveau.md` (G-01) sont alignés ; les scripts E2E et le mock `liorian-connect`
  publient désormais des archives `.LiorArtifactPackage`.

### Fixed
- **Empreinte de manifeste signée : le document, plus la structure canonique** — `pack`, `sign` et `publish` signaient `ManifestChecksum(struct)` — une reconstitution canonique de 1
  330 octets — alors que l'archive embarque le `manifest.json` verbatim et que le store, `api-core` et
  le socle recalculent `manifestChecksumOf(document)` : SHA-256 de
  `canonicalJson(JSON.parse(fichier))`. Aucune publication ne pouvait donc aboutir, et une signature
  produite en local ne se vérifiait nulle part. La chaîne signe désormais le document ; `publish`
  recalcule l'empreinte après les écritures de normalisation du manifeste et après tout bump de
  version, au lieu de reporter un état périmé. Verrouillé par 4 tests de contrat, dont une valeur de
  référence calculée par le contrat partagé.
- **Identifiant catalogue unique sur toute la chaîne** — `pack`, `sign`, `sign verify`, `publish` et l'installateur local divergeaient sur la reconstruction
  de `mod.<publisher>.<module>` — le packer et l'éditeur signaient l'identifiant, `sign verify`
  recalculait `manifest.domain`, l'installateur codait `developer` en dur : une signature valide en
  local était illisible partout ailleurs. `store.ResolveModuleIdentifier` impose un ordre d'autorité
  unique (compte authentifié, puis `publisher.id`, puis le libellé du domaine si la forme
  `mod.<x>.<y>`, puis le défaut du store) et écarte un `publisher.id` en UUID — identifiant
  d'enregistrement, jamais slug de catalogue — qui garantissait un rejet à la publication.
- **Résolution de module unique (`config.ResolveModuleDir`)** — `pack`, `sign`/`sign verify`, `publish` et `link` partageaient leur propre recherche d'arbre. Ils
  passent par un résolveur unique qui privilégie l'arbre source `modules/`, puis l'arbre
  d'installation `library/modules/`, et sait enfin retrouver un module par l'identité déclarée dans
  son manifeste (`id` ou `domain`) — ce qui rend exécutable l'instruction affichée par `create module`
  (`liora pack <domain>`) sur un module de l'arbre source.
- **Scaffold conforme et créé dans l'arbre source** — un module créé par `liora create module` n'avait pas de `tsconfig.json` — donc un `LevelError` de la
  règle D6-2 dès le premier `pack` en arbre isolé —, son service étendait `ApiService` (abstraite à
  instance, `assertAllowed`), ses vues importaient l'alias interne au socle `@/core/…` et sa
  déclaration ne portait pas le champ `external` exigé par `ModuleDeclarationInterface`. Le mockup
  embarqué fournit maintenant un `tsconfig.json` autonome (JSON strict, contrat interne du SDK,
  `paths` vers les sources du SDK, sans `extends` : un module créé hors monorepo n'a aucune base à
  hériter) ; `create module` vise `modules/<id>` quand l'arbre source existe, seul arbre où le
  `package.json` du module est installé et donc le seul où le typecheck s'exécute. Vérifié par
  exécution : module créé, `tsc --noEmit` 0 erreur, archive signée, verdict `verified` du socle.
  Quatre tests verrouillent l'option par option et interdisent la réapparition d'un alias hors de
  portée.
- **Audit à zéro sur les 14 modules first-party** — 222 erreurs et 25 avertissements ramenés à 0 erreur / 0 avertissement : grammaire
  `userScope`/`permissions` alignée sur celle qu'impose l'audit, règle « domain directory » qui
  autorise la forme `mod.liorian.chating` d'un module existant, scan bloquant des sources avec
  observation des hits dans le bundle, politique `publisher` (bloc vide = sans finding, bloc à moitié
  rempli = avertissement), `ModuleExists` connaissant les 4 arbres, et une boucle infinie de
  `runTypecheck` supprimée. `publisher: {id: "", name: ""}` n'est plus compté comme une identité
  déclarée.

## [v0.26.0] - 2026-09-28

### Added
- **Commande `liora typecheck`** — pas de porte utilisateur pour la règle 2 du pack
  (spec `module-isolated-runtime.md` §4.4) : `liora typecheck [module]` exécute le contrôle
  TypeScript `tsc --noEmit` sans produire d'archive, en réutilisant le moteur fail-closed de
  `liora pack` (`module.RunTypecheck`, désormais exporté) : script `typecheck` du module en
  priorité, `tsc`/`bun` en repli, et un toolchain indisponible reste une erreur — jamais un succès
  silencieux. Le module est résolu depuis l'arbre workspace (`modules/<id>`) puis depuis l'arbre
  d'installation (`library/modules/<id>`), avec sélecteur interactif en l'absence d'argument.
- **Entrée canonique `main.tsx` et payload de développement sous `.liorian/artifact/` (D7)** —
  `manifest.entry` prend `main.tsx` comme nom canonique (`config.ModuleEntryFileName`) et le build
  écrit son payload exécutable dans `modules/<id>/.liorian/artifact/`
  (`config.ModuleArtifactSourceDir`), séparant l'arbre de développement de la distribution :
  l'archive `.liozip` conserve le préfixe `artifact/**` et l'installation le dépose dans
  `library/modules/<id>/<version>/artifact/**`. `Manifest.SourceArtifactDir` résout le répertoire
  sur le disque (`.liorian/artifact` → déclaration du manifeste → `artifact`) et
  `isModernModuleDir` reconnaît les deux layouts.

### Changed
- **Domaine canonique `mod.<organization-slug>.<module-identifier>`** — la forme canonique exigée
  par la spec `module-installation.md` §4.3 est contrainte à exactement trois labels kebab-case :
  `mod.acme.crm.extra`, `mod.acme` et `mod.acme._bad` ne sont plus reconnus comme canoniques et
  remontent en avertissement de validation au lieu d'être acceptés en silence. Le mockup de
  référence `hello-world` est réaligné sur `mod.liorian.hello-world`.
- **Suppression des plafonds de taille de l'artefact (règle 6 de §4.4)** — la validation ne refuse
  plus que le payload vide (`artifact/module.js`, `artifact/index.html`) : les seuils de 5 Mo
  (bundle) et 25 Mo (arbre complet), ainsi que le parcours de directory associé, ont disparu, la
  taille de l'artefact relevant de la responsabilité du développeur.

### Fixed
- **`liora repair` préserve le point d'entrée runtime des modules isolés** — la normalisation de
  `manifest.entry` reconnaît `entry.tsx` (première révision du runtime isolé) via la nouvelle
  constante `config.LegacyEntryFileName` au lieu de retomber sur la déclaration `index.tsx` : un
  module isolé existant n'est plus requalifié en module legacy par la commande de réparation.

## [v0.25.0] - 2026-09-27

### Added
- **Runtime isolé des modules — artefacts exécutables et installation locale (phase 2, spec
  `module-isolated-runtime.md`)** — `liora pack` d'un module workspace (`modules/<id>/`, D5) émet
  l'archive au contrat artefact : `manifest.json` à la racine, `src/**` (source TS, D4) et
  `artifact/**` (payload exécutable de l'iframe). Validation bloquante au pack : entrée
  TypeScript obligatoire (D6), `tsc --noEmit` vert (via le script `typecheck` du module ou
  `tsc`/`bun`), `artifact/module.js` + `artifact/index.html` présents et non vides, plafonds de
  taille, aucun import `next/*` (D1) ni alias `@/` (§7.7), aucun `fetch`/`XMLHttpRequest`/
  `WebSocket` dans le bundle (D16), `userScope` obligatoire et conforme à la grammaire
  `Role:Verbe` (D15), déclarations `backends` conformes au schéma (D9 : clé kebab-case, URL
  https sauf loopback, ni userinfo ni traversée). Le manifeste porte désormais les sections
  typées `userScope`, `backends` et `artifact` (fin du passage silencieux par `Extra`).
  `allowedEntry` accepte `src/**` et `artifact/**` et **refuse** le layout historique
  `src/app/**` / `public/assets/**` à l'installation. Les archives s'installent **multi-version**
  dans `library/modules/<id>/<version>/` avec un pointeur de version active `current` (D11) —
  prérequis du rollback local. Nouvelle commande **`liora install <archive.liozip>`**
  (`internal/localinstall`) : installation d'un fichier local sans marketplace ni api-core, même
  moteur fail-closed, sidecar `.sig` vérifié contre le trousseau quand il existe. La résolution
  de modules (`liora pack`, audit, gate, sélecteur) reconnaît l'arbre workspace `modules/*` et
  les installations multi-version ; `modules/hello-world` sert de template de référence.
## [v0.24.0] - 2026-09-26

### Added
- **Chaîne de signature alignée sur le serveur** — `internal/pkg.CanonicalJSON` sérialise la charge
  signée en JSON canonique (clés triées récursivement, sans échappement HTML), contrepartie Go
  attendue du `canonicalJson` partagé côté serveur : toute divergence de sérialisation rendait
  l'artefact invérifiable (spec module-installation §7.1). `internal/signing` tient un registre
  local de liaisons (`signing-bindings.json`, sans secret) associant chaque domaine
  `mod.<publisher>.<module>` à la clé qui le publie (empreinte, id de clé, date). `liora sign`
  accepte un module ou un chemin d'archive, signe la charge canonique en Ed25519 (empreinte
  SHA-256 de l'archive + manifeste), synchronise la clé publique sur le serveur en meilleur
  effort et lie la clé au domaine (rotation confirmée si une autre clé était déjà liée) ;
  `liora pack` signe l'archive fraîchement produite et `liora verify` retrouve la clé liée au
  domaine.
- **Identité développeur résoluée à la publication** — `internal/store/publisher.go` expose
  `Client.GetMyAccount()` (`GET /api/developer-store/accounts/me`, réponse `DeveloperAccountVm`)
  et `cmd/publish.go` s'en sert pour remplir `manifest.publisher` : `publisher.id` est
  l'identifiant de compte Liora que `liorian-connect` scope sur
  `DeveloperProduct.developerId`, **pas l'identifiant Apple ni celui d'un autre
  constructeur**. Repli sur l'`id` de session (keychain) si l'endpoint est injoignable, et
  court-circuit quand le manifeste est déjà complet. Le récapitulatif affiche désormais
  l'éditeur (`label.publisher`).
- **Commande `liora module` (cycle de vie Developer Store)** — sous-commandes `knowledge`,
  `workflows`, `channels`, `platforms`, `requirements`, `signing-keys`, `accreditations`,
  `variables`, `github` (option `--connect`) et `observer`/`usage`, adossées aux endpoints
  `/api/developer-store/*` de `liorian-api-connect` (client `internal/store/platform.go`). Le
  module local est résolu par son token de manifeste, l'authentification réutilise la session
  `liora connect`, et la sortie est restituée en tableaux `tui`.
- **Contrat CLI complet du cycle de vie (E-008)** — les commandes deviennent opérationnelles et
  plus seulement en lecture : `module list`, `knowledge list|add|publish [--draft]`,
  `workflow list|run|delete`, `channels list|publish|rollback|pause`,
  `platforms [get]|set --web/--desktop/--mobile`, `requirements list|add`,
  `signing-keys [list]|rotate`, `accreditations list|add`, `variables list|set|unset`,
  `github [status]|link|unlink`, `observer` et `usage`. Les identifiants (workflow, article)
  peuvent être passés via `--id` pour un usage non interactif ; les suppressions demandent une
  confirmation sauf `--force`. `variables set` crée la variable si absente, la met à jour sinon.
- **Validation SemVer des canaux** — `module channels publish <canal> <version>` rejette toute
  version non-SemVer (`pkg.IsSemver`) et tout canal inconnu parmi `ALPHA|BETA|NIGHTLY|RC|RELEASE`.
- **Cycle de vie complété (spec `module-lifecycle` §1–14)** — les ressources restantes de la spec
  disposent désormais d'une commande CLI : `dev-builds list|add|delete`, `fingerprints
  list|add|delete`, `caches list|purge|delete`, ainsi que les mutations manquantes
  `knowledge delete`, `workflow add|update`, `requirements delete`,
  `signing-keys create|delete` et `accreditations delete` (avec `--status`/`--expires-at`).
  Les identifiants sont passés via `--id` (non interactif) et les suppressions/purges exigent
  `--force`.
- **Signature des archives à la publication** — `liora publish` signe désormais l'archive
  `.liozip` avec la clé de signature locale avant l'envoi et vérifie la signature produite ; en
  l'absence de clé, la publication se poursuit non signée avec un avertissement (spec §5.6 /
  SEC-009).
- **Flag `--color`** — force l'activation des couleurs (profil le plus riche supporté par le
  terminal), y compris lorsque la sortie est redirigée ou que `NO_COLOR` est défini. `--no-color`
  reste prioritaire lorsque les deux flags sont fournis ; la préférence est persistée dans
  `liorian.config.json` sous `cli.noColor`.
- **Révocation OAuth2 à la déconnexion** — `liora disconnect` révoque les jetons d'accès et de
  rafraîchissement auprès de l'endpoint `/oauth/revoke` (RFC 7009), en meilleur effort
  (spec §8.1 / §6.3).
- **Developer Store adossé à `liorian-connect`** — le store résout sa base URL depuis
  `app.config.json` (`liorian-connect`) ou la variable `LIORIAN_CONNECT_API`, et expose des
  métadonnées enrichies : statut du produit, développeur et dernière version publiée par module.

### Changed
- **Le CLI s'appelle `liora` (rupture)** — le binaire, la formule Homebrew (`Formula/liora.rb`)
  et toute la documentation passent de `liorian` à `liora` ; les applications décrites dans
  `app.config.json` sont rebaptisées « Liora Socle / Connect / Console / Store / Auth ».
  L'installateur npm (`lib/install.js`) conserve toutefois un alias `liorian` vers le même
  binaire pour préserver les scripts et la CI existants. Les identifiants techniques ne bougent
  pas (`liorian-connect`, variables `LIORIAN_*`).
- **Les archives passent au format `.liozip` (rupture)** — ADR-003 de la spec module-installation :
  `liora pack` produit désormais `<name>-<version>.liozip` (un ZIP renommé) au lieu de `.SenMod` ;
  les extensions `.SenMod`/`.smp` restent acceptées en entrée (recherche de signature,
  installation marketplace) mais ne sont plus produites.
- **`liora publish` ne demande plus l'identifiant développeur** — l'invite `publish.prompt.dev_id`
  est retirée (clés `fr-FR`/`en-US` supprimées) : la valeur était saisie à l'aveugle alors que le
  bloc `publisher` n'est jamais transmis au store (`CreateModuleProductDto` ne l'expose pas).
  Seuls `name`, `description` et `publisher.name` restent demandés, et le `manifest.json` est
  désormais persisté hors mode interactif.

### Fixed
- **Publication : `POST /api/developer-store/modules` renvoyait 404** — `app.config.json` pointait
  `liorian-connect.api.baseUrl` sur `https://localhost:5711` (`liorian-api-core`) au lieu de
  `https://localhost:5721` (`liorian-api-connect`). Le client de publication CreateProduct
  interrogeait donc un service qui n'expose pas le developer store, et `liora publish` échouait
  sur `failed to create the remote module: Not Found (HTTP 404)`. `liorian-console.api.baseUrl`
  était aligné sur la même valeur erronée (`https://localhost:5741` attendu).
- **Diagnostic trompeur sur une 404 de publication** — une 404 sur la route de collection est
  désormais signalée par l'erreur sentinelle `store.ErrNoStoreRoute` (citant l'URL de base
  utilisée) au lieu d'un « Not Found » brut, et `cmd/publish.go` affiche une piste dédiée
  (`publish.error.no_route.fix`) : l'URL de base résolue n'expose pas le developer store, il faut
  viser `liorian-api-connect` via `app.config.json` ou `LIORIAN_CONNECT_API` — ce n'est pas un
  problème de connexion.
- **Erreurs applicatives Raiton en HTTP 200** — une enveloppe Raiton `error: true` (validation
  DTO, erreur métier) renvoyée avec un statut HTTP 200 est désormais remontée comme `APIError`
  avec son code et son message, au lieu de produire un `data: null` silencieux et une 404
  trompeuse en aval.

### Technical Details
- `internal/pkg/canonical.go` : `CanonicalJSON` (JSON canonique identique à l'implémentation
  serveur) ; `internal/signing/bindings.go` : registre `signing-bindings.json` (`LookupBinding`,
  `BindBinding`, `RemoveBinding`, `ListBindings`) ; `cmd/sign.go` : `resolveSignTarget`,
  `signingPayload`, `bindModuleKey`, `rotatePreviousKey`, `verifyTarget`, signature de l'archive
  fraîchement packée dans `cmd/pack.go`.
- `internal/store/platform.go` : modèles `DevBuild`, `Fingerprint`, `CacheEntry` et opérations
  d'écriture du cycle de vie (`CreateKnowledgeArticle`, `PublishKnowledgeArticle`,
  `DeleteKnowledgeArticle`, `Create`/`Update`/`DeleteWorkflow`, `Create`/`DeleteDevBuild`,
  `Create`/`DeleteFingerprint`, `Purge`/`DeleteCache`, `RollbackChannel`/`PauseChannel`,
  `SaveModulePlatforms`, `Create`/`DeleteRequirement`, `Create`/`Rotate`/`DeleteSigningKey`,
  `Create`/`DeleteAccreditation`, `Create`/`Update`/`DeleteEnvironmentVariable`).
- `cmd/module.go` : arborescence de sous-commandes, helpers `normalizeChannel`, `platformRows`,
  `findVariable`, `workflowLabel`, `articleTitle`, `requirementLabel`, `upperAll`, sélections
  interactives (`selectKnowledgeArticle`, `selectWorkflow`), réutilisation de `slugify`
  (`cmd/init_env.go`).
- `internal/pkg/semver.go` : ajout de `IsSemver` (tolérant au préfixe `v`).
- `e2e/mockapi` : endpoints du cycle de vie pris en charge (knowledge, workflows, dev-builds,
  channels, fingerprints, caches, platforms, requirements, signing-keys, accreditations,
  environment-variables — étatful — github, observer, usage) ; scénario `16_module.txtar` (TC-030).
- `internal/i18n/locales` : clés FR/EN des nouvelles commandes et messages.
- `internal/auth/oauth.go` : `RevokeToken` (RFC 7009, tolérant au HTTP 400).
- `cmd/publish.go` : `signForPublish` (signature + vérification via `internal/signing`).
- `cmd/root.go` / `cmd/localize.go` : flag `--color`, `colorFromArgs` /
  `colorPreferenceFromArgs`, `forcedColorProfile`.
- `internal/pkg/http.go` : champs `error` / `code` de l'enveloppe Raiton pris en compte sur les
  réponses 2xx.
- `internal/store/publisher.go` : client Developer Store dédié (`liorian-connect`),
  `LIORIAN_CONNECT_API`, métadonnées produit enrichies.
- `internal/appconfig/appconfig.go` : constante `ConnectAppID`.
- `go.mod` : `golang.org/x/text` devient une dépendance directe (normalisation Unicode dans
  `internal/store/publisher.go`).

## [v0.21.0] - 2026-09-21

### Added
- **Génération du `.env` pendant `liorian init`** — le `.env` est désormais produit à partir du
  fichier d'exemple du template (`.env-sample`, `.env.sample`, `.env.example`, `.env.dist` ou
  `.env.template`, premier trouvé). Les variables connues sont synthétisées : clé applicative
  (`APP_KEY` / `*ENCRYPTION_KEY`, 32 octets hex), nom et slug du projet (`*APP_NAME`,
  `*APP_SLUG`), paire VAPID pour le Web Push (`*VAPID_PUBLIC_KEY` + clé privée associée). Un
  `.env` existant n'est **jamais écrasé** ; le fichier écrit conserve les commentaires, l'ordre
  et le style de citation de l'exemple, avec des permissions `0600`.
- **Flag `--auto-env` / variable `LIORIAN_CLI_ENV_AUTO`** — configure entièrement `init` sans
  question (nom du projet déduit du dossier courant, gestionnaire de paquets recommandé, `.env`
  accepté tel quel). Sans ces options, `init` propose d'abord d'accepter toutes les valeurs
  suggérées ; en cas de refus, chaque variable restante est demandée avec sa suggestion en
  placeholder.
- **Touche `Échap` pour auto-remplir** — dans les invites de saisie de `init`, `Échap` conserve
  la valeur saisie et accepte la suggestion pour toutes les variables restantes (indice
  `esc: auto-fill the rest`). Nouvelle API `tui.AskTextAuto` (distincte de l'annulation
  `Ctrl+C`).

### Technical Details
- `internal/pkg/env.go` : parseur/sérialiseur dotenv préservant commentaires, lignes vides et
  ordre (`ParseEnv`/`EnvFile`/`Render`), détection de l'exemple (`FindEnvSample`),
  `NewEncryptionKey` et `NewVAPIDKeys` (ECDSA P-256, base64url sans padding).
- `cmd/init_env.go` : `planEnvFromSample`/`synthesizeEnv`/`completeEnv`, helpers `slugify`,
  `envKeyLabel`, `vapidPrivateKey` ; l'étape `.env` s'exécute hors du spinner (une invite
  Bubbletea ne peut pas posséder le terminal pendant le spinner).
- `cmd/init.go` : étape 7 (`.env`) + ligne « Environment: .env » dans la carte de résumé.
- `internal/tui/prompts.go` : `inputQuit` (`Enter`/`Esc`/`Cancel`) et `askInput(... autoEscape)`.


## [v0.20.0] - 2026-09-21

### Added
- **Commande `repair`** — `liorian repair` corrige automatiquement les anomalies réparables d'un
  module (champs de manifeste, dépendances npm manquantes, JSON malformé) en réutilisant le
  pipeline d'audit, puis re-audite le module ; les points non réparables deviennent des
  instructions pas à pas. Options `--dry-run`, `--warnings`, `--no-install`,
  `--no-interaction`, `--output table|json`.
- **Commande `create view`** — `liorian create view <module> [name]` génère une vue de
  présentation (`presentation/views/<name>.view.tsx`) à partir du mockup embarqué, avec le
  composant, le titre et la description renommés (`--mockup`, `--name`, `--label`,
  `--description`).
- **Métadonnées de release pendant le téléchargement du socle** — `liorian init` affiche
  désormais la version (tag), le canal, la branche cible et le commit de la release téléchargée
  sur une ligne atténuée **sous** la barre de progression
  (`release v0.23.0-alpha.1 · channel alpha · branch 71892a5a… · commit 71892a5a…`).
  Les métadonnées sont résolues via l'API GitHub avant le téléchargement (branche/commit
  best-effort : `inconnu` si indisponibles) puis réutilisées pour éviter un second appel réseau.

### Changed
- **Dépendances npm portées par le `package.json`** — les champs `dependencies` et
  `devDependencies` sont retirés du `manifest.json` (struct Go, mockup `hello-world` et
  manifestes générés) : le `package.json` du module devient la source unique. L'audit vérifie
  désormais l'installation des dépendances runtime déclarées dans ce `package.json`.
- **Installation forcée des dépendances `latest`** — après le téléchargement du socle
  (`liorian init`) comme à la création d'un module, les dépendances explicitement versionnées
  `latest` sont réinstallées explicitement (`<pm> add <pkg>@latest`) car un simple `install`
  les ignore souvent (lockfile / paquet déjà présent).
- **Porte de santé de l'outillage limitée à l'audit** — `liorian dev`, `build` et `start`
  n'exécutent plus les contrôles `debug` et `test` avant le script ; seule la conformité
  (`audit`) est vérifiée (TFC-015/-016). Les contrôles `debug`/`test` restent des commandes
  dédiées (`liorian debug`, `liorian test`).
- **Domaine de module libre** — le champ `domain` du manifeste n'a plus à respecter le préfixe
  `mod.liorian.<name>` : tout domaine reverse-DNS valide (`com.organization.domain`) est accepté
  par l'audit et la validation.
- **`repair` corrige le nom du dossier du module** — quand le dossier ne correspond pas au
  domaine du manifeste, `repair` propose le nouveau nom dans un prompt (touche `tab` pour le
  remplir automatiquement), puis renomme le dossier. Avec `--no-interaction`, la proposition est
  appliquée sans question ; hors terminal, elle est reportée en instruction manuelle.
- **Présentation des rapports `audit` et `repair`** — les deux commandes partagent désormais le
  même rendu : en-tête avec verdict aligné à droite (`✓ passed` / `✗ failed`), tableau des
  **points d'action** uniquement (les contrôles réussis sont résumés en « N check(s) passed »),
  décompte compact par sévérité et carte de synthèse. Les instructions manuelles de `repair`
  affichent la catégorie · la règle, le message puis les étapes.

### Technical Details
- `internal/pkg/nodepackage.go` : type `NodePackage` + `LoadNodePackage`,
  `DependencyNames`/`RuntimeDependencyNames`/`LatestDependencies` et `ForceInstallLatest` ;
  `internal/pkg/pm.go` : `DependencyArgs` (bun/pnpm/yarn `add`, npm `install`).
- `internal/module/manifest.go` : suppression des champs `Dependencies`/`DevDependencies` ;
  `internal/audit/auditor.go` : dépendances lues depuis `library/modules/<module>/package.json` ;
  `internal/module/creator.go` : `ResolveDependencies(moduleDir)` + forçage des `latest`.
- `internal/pkg/github.go` : type `ReleaseInfo` + `ResolveRelease`/`DownloadReleaseZip` ;
  `resolveTagCommit` déréférence les tags annotés via `git/ref/tags` + `git/tags`.
- `internal/tui/progress.go` : `RunWithProgressDetail` rend une ligne de détail atténuée
  (`Styles.Muted`) sous la barre (et sous la coche une fois terminé).
- `cmd/init.go` : helper `releaseDetail` et résolution en amont ; `githubOwnerRepo` restreint aux
  URLs `github.com` (les URLs ZIP directes retombent sur le téléchargement direct).
- `internal/tui/report.go` : nouveaux helpers `ReportHeading`, `StatusChip` et `CountsLine` ;
  `internal/module/validator.go` : `Result.OKCount()`.
- `cmd/gate.go` : `gateChecks` réduit à `audit` pour `dev`/`build`/`start` ; `gateDebug` et
  `gateTest` conservés pour d'éventuels usages.
- `internal/module/validator.go` : règle `domain` validée par `isDomainName` (reverse-DNS) ;
  suppression de `isLiorianDomain`.
- `internal/repair/repair.go` : `Repairer.NoInteraction` et `Repairer.Rename` (callback de
  prompt) ; `applyRename`/`renameModuleDir` renomment le dossier, remappent le résultat et les
  installations de dépendances en attente.
- `cmd/repair.go` : flag `--no-interaction` et helper `repairRenamePrompt` (placeholder =
  suggestion, `tab` pour remplir).
- `internal/module/view.go` + `cmd/create_view.go` : `ViewCreator`/`ViewSpec` et commande
  `create view`.


## [v0.19.0] - 2026-09-20

### Added
- **Build info aligné sur `app.config.json`** — la version, la branche, l'id de commit et la date
  proviennent désormais du registre `app.config.json` embarqué (nouvelles clés `branch`, `commit`,
  `date`), qui devient la source de vérité d'un build local ; les `ldflags` (GoReleaser, script
  `dev-install`) restent prioritaires pour les builds de release.
- **`--version` enrichie** — `liorian -v` / `--version` affiche désormais
  `liorian v<version> (<os>/<arch>) <branche> (<commit>)`.

### Changed
- **Variables de compilation** — `main.branch` et `main.commit` coexistent désormais ; le template
  GoReleaser passe de `{{.Commit}}` à `{{.Branch}}` + `{{.ShortCommit}}`.
- **Version bump** — `app.config.json` et le wrapper npm passent sur `0.19.0`.

### Docs
- `README.md`, `docs/specs/liorian.md` et le rapport d'implémentation alignés sur les nouveaux
  champs de configuration et la sortie de version.

### Technical Details
- `main.go` : helper `buildInfo()` qui résout version/branche/commit/date depuis le
  `app.config.json` embarqué, en laissant la priorité aux valeurs injectées par `ldflags` ; champs
  `Branch`, `Commit` et `Date` ajoutés à `internal/appconfig.Config`.
- `cmd/root.go` : signature `Execute(version, branch, commit, date, appConfig)` et template de
  version Cobra mis à jour ; scénario E2E `01_help_version.txtar` rendu tolérant au SemVer.
- `internal/pkg/update_test.go` : les tests de notification de mise à jour forcent le contrôle
  réseau via `LIORIAN_CLI_UPDATE=1` (robustesse en CI).

## [v0.18.0] - 2026-09-20

### Added
- **Commandes outillage `dev`/`build`/`start`/`check` (spec `docs/specs/liorian-toolchain.md`)** —
  passe-plat vers les scripts `package.json` du projet via le gestionnaire de paquets choisi à
  l'installation (`dev`→`dev`, `build`→`build`, `start`→`start`, `check`→`lint`). Les arguments
  après `--` sont transmis au script (`npm` insère `--`). Actions `before`/`after` configurables
  dans la section `toolchain` de `liorian.config.json` (`commands`, `before`, `after`) ; un échec
  de `before` abandonne l'opération, `after` s'exécute même si la commande échoue. Exit codes :
  `1` résolution impossible, code de la commande propagé, `130` interruption (Ctrl+C).
  `dev`/`start` (serveurs) s'arrêtent via Ctrl+C, `build`/`check` sont one-shot. Le moteur du
  socle n'est jamais nommé dans les interfaces utilisateur.
- **Porte de santé des modules avant les commandes outillage (spec §5.7, TFC-015/-016/-017)** —
  `liorian dev` vérifie d'abord les modules (`debug` + `test`), `liorian build`/`start` ajoutent
  `audit` (`debug` + `test` + `audit`) avant de proxier le script `package.json` : un module en
  erreur annule la commande (warnings non bloquants ; sans module dans `library/modules/`, le
  contrôle est ignoré). Chaque contrôle s'affiche en étapes live et, en cas d'échec, détaille les
  modules fautifs puis renvoie le code du contrôle (debug `10`, test `13`, audit `1`) avec un
  indice vers la commande standalone (`liorian debug` / `test` / `audit`). Le contrôle est non
  interactif (pas de sélection de package de test) ; `check` reste exempté.

### Changed
- **Dossier des modules `library/modules/` (rupture)** — le répertoire `external_modules/` est
  renommé `library/modules/` : tout l'outillage (`init`, `create`, `pack`, `sign`, `debug`,
  `test`, `audit`, `link`) et le harnais E2E sont alignés sur le nouveau chemin. Les projets
  existants doivent déplacer leurs modules vers `library/modules/`.
- **Références d'organisation harmonisées sur `protorians`** — le module Go passe sur
  `github.com/protorians/lior-cli`, le paquet npm sur `@liorian/cli` (au lieu de `@lior/cli`),
  les domaines d'application sur `*.liorian.protorians.com`, le tap Homebrew sur
  `protorians/lior-cli` (formula, GoReleaser, README) et l'auto-update sur les releases
  `protorians/lior-cli` ; les derniers reliquats `jetbrains` sont purgés du dépôt.
- **Version bump** — `app.config.json` et le wrapper npm passent sur `0.18.0`.

### Docs
- Nouvelle spec `docs/specs/liorian-toolchain.md` (commandes outillage, hooks `before`/`after`,
  porte de santé, config `toolchain`) et synchronisation de `docs/specs/liorian.md`, du
  `README.md` (commandes, chemin `library/modules/`) et du rapport d'implémentation.

### Technical Details
- Nouveaux packages `internal/toolchain` (exécution des scripts via le gestionnaire de paquets,
  hooks, interruptions Ctrl+C) et `internal/module` aligné sur `library/modules/` ; commandes
  Cobra génériques `cmd/toolchain.go`, porte de santé `cmd/gate.go` et scénario E2E
  `14_toolchain.txtar` (dev/build/start/check, hooks, échecs propagés).

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
- **Préférences CLI persistées (spec §5.11)** — `--lang`, `--no-color` et `--verbose`
  mémorisent désormais leur choix dans `liorian.config.json` (`cli.lang`, `cli.noColor`,
  `debug.verbose`) à chaque exécution dans un projet Liorian : le choix est appliqué à tous
  les lancements suivants sans repasser les flags.
- **Nouvelle clé `cli.noColor`** — désactive les couleurs de sortie au niveau projet. La
  priorité reste `flag --no-color[=true|false]` → env → config : le flag écarte la config
  (lu dès l'aide/version, avant même l'analyse Cobra) et `--no-color=false` permet d'effacer
  une valeur persistée.

### Changed
- **Menus de sélection ajustés au terminal** — la question reste toujours visible en tête
  d'écran : la liste reserve la hauteur du titre et de la barre de navigation, son en-tête et sa
  pagination génériques sont masqués, et aucune option n'est plus rognée sur les petits
  terminaux. Les questions des invites (saisie, sélection, confirmation) partagent le même style.
- **Version bump** — `app.config.json` et le wrapper npm passent sur `0.17.0`.

### Technical Details
- Réorganisation de `collectCreateSpec` (validation par étape, extraction de `askValidated`),
  extraction de `newSelectModel`, et suppression du style `DimTitle` au profit de `Question`.

### Docs
- Synchro de la spec §5.11 (`liorian.config.json`) avec la clé `cli.noColor` et la
  persistance des flags.

## [v0.16.1] - 2026-09-20

### Fixed
- **Distribution Homebrew (spec §10.2)** — la formula est renommée `Formula/lior-cli.rb` →
  `Formula/liorian.rb` (nom de la formula aligné sur le binaire installé `liorian`) : la
  commande d'installation devient `brew install protorians/lior-cli/liorian` (référence de
  tap en 3 segments). `README.md`, spec §10.2, notes de release GoReleaser et
  `scripts/release-notes.sh` alignés ; la formula `lior-cli.rb` est supprimée.

## [v0.16.0] - 2026-09-20

### Fixed
- **Auto-update (S-015 / NFR-006) — la notification n'apparaissait jamais** : la comparaison de
  version dans `pkg.CheckForUpdate` était inversée (`isNewer` faisait retomber sur une chaîne
  vide, donc aucune mise à jour n'était jamais signalée). Correctif et couverture de bout en bout.

### Added
- **Auto-update documenté et testé (S-015 / NFR-006)** — notification seule, **pas de mise à jour
  forcée ni de téléchargement automatique** de la nouvelle version :
  - endpoint du check surchargeable via `LIORIAN_CLI_UPDATE_URL` (tests hermétiques, miroirs,
    serveur de releases auto-hébergé) en plus du défaut GitHub releases (cache 24 h) ;
  - désactivation inchangée : `LIORIAN_CLI_SKIP_UPDATE` (ou `CI` sans opt-in `LIORIAN_CLI_UPDATE`) ;
  - tests unitaires `internal/pkg/update` exercent désormais `CheckForUpdate` de bout en bout
    (mock HTTP : notification, à jour, erreur silencieuse, cache, skip) et **garantissent un unique
    GET / aucune requête de téléchargement** ;
  - test E2E `e2e/update_test.go` (`TestUpdateNotification`) : binaire versionné, notification
    `Update available: v0.14.0 → v99.0.0` + lien releases, exactement une requête GET.
- **Distribution Homebrew (spec §10.2)** — formula `Formula/lior-cli.rb` (tap = dépôt
  `protorians/lior-cli`) avec archives darwin/linux amd64/arm64, shas et `brew test` :
  "brew tap protorians/lior-cli https://github.com/protorians/lior-cli.git" puis
  `brew install protorians/lior-cli/lior-cli` (la forme à 2 segments `brew install
  protorians/lior-cli` n'existe pas dans Homebrew : référence de tap en 3 segments).

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
    de façon sûre dans `library/modules/`. Refuse les modules inconnus (exit 3), les doublons
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
  désormais la section `debug` de `liorian.config.json` (`debug.verbose: true` ou
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
- **Nom commercial `Lior` / code de projet `liorian`** — le produit reste commandé `liorian`, mais ses surfaces publiques sont réparties
  en trois strates : **nom commercial `Lior`** (module Go `github.com/protorians/liorian-cli` → `github.com/protorians/lior-cli`,
  paquet npm `@liorian/cli` → `@lior/cli`, artefacts de release `liorian-cli_*` → `lior-cli_*`, client OAuth `lior-cli`) ;
  **code de projet `liorian`** (fichiers `liorian.config.json`/`liorian.config.toml` → `liorian.config.*`, schéma
  `liorian.config.schema.json` → `liorian.config.schema.json`, dossier projet `.liorian/` → `.liorian/`, vault `~/.liorian-cli` →
  `~/.liorian-cli`, services keychain `liorian-cli`/`liorian-cli-signing` → `liorian-cli`/`liorian-cli-signing`) ;
  **commande `liorian`** inchangée (binaire, wrapper npm, aide `Lior CLI`, variables `LIORIAN_*`).
- **L'écosystème reste `Liorian`** — applications plateforme (`Liorian Socle/Connect/Console/Store/Auth`), domaines
  `*.liorian.protorians.com`, template `protorians/liorian-socle`, domaine d'audit `mod.liorian.*` et SDK `@liorian/sdk`
  ne changent pas de nom.
- **Rupture** — les fichiers de configuration et chemins internes `liorian*` sont renommés `liorian*` ; les scripts et CI
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
- **`create module` par domaine** — le module est scaffolé sous `library/modules/<domain>/` (et `public/assets/<domain>/`) et adressé partout par son **domaine** (pack, sign, debug, audit, link) ; les modules requis peuvent aussi être résolus par leur `id` de manifest.
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
- **`create module` — vérification des `requirements`** : chaque module requis par le `manifest.json` doit exister localement — dans `library/modules/` **ou** `src/modules/` ; les modules cœur de la plate-forme (`organization`, `identity`) sont toujours satisfaits. En cas de module manquant, la création échoue et le module scaffoldé ainsi que la page éventuelle sont supprimés (rollback), avec une erreur catégorisée listant les modules absents (`create.error.requirements_missing`).
- **`create module` — installation des dépendances** : les `dependencies` et `devDependencies` du manifest sont résolues via le premier gestionnaire de paquets détecté (`bun → pnpm → yarn → npm`) à la racine du projet. Désactivable via `LIORIAN_CLI_SKIP_INSTALL=1` ; un échec d'installation ou l'absence de gestionnaire reste non-bloquant (simple avertissement).
- **Manifest** — prise en charge du champ `devDependencies`, ajouté au mockup embarqué hello-world.

### Changed
- **Audit** — la vérification des requirements réutilise le moteur de résolution commun à `create` (`library/modules/` **ou** `src/modules/`).

### Technical Details
- **CI / Release** — les workflows GitHub sont reconstruits de zéro : pipeline de release déclenché par un tag `vX.Y.Z` (créable aussi via `workflow_dispatch`), build des binaires GoReleaser Windows/macOS/Linux dans `./dist` (archives + binaires nus + `checksums.txt`), publication automatique de la release GitHub dont le sommaire contient les liens de téléchargement des binaires et les détails du changelog (`scripts/release-notes.sh`).

### Docs
- `docs/specs/liorian.md` §5.2 mis à jour (vérification des requirements et installation des dépendances dans le pipeline `create module`) ; `docs/rapport-implementation.md` complété.


## [v0.2.0] - 2026-09-15

### Added
- **`create module --mockup` / `--page-mockup`** — le module et la page sont scaffolés depuis des répertoires/gabarits explicites (priorité sur `LIORIAN_MODULE_MOCKUP` / `LIORIAN_PAGE_MOCKUP`), avec repli silencieux sur les mockups embarqués si la source est inutilisable.

### Changed
- **Mockup hello-world aligné 1:1 sur le socle** — le mockup embarqué est désormais identique au module de référence `liorian-socle/library/modules/hello-world` (API SDK `View.*` / `Activity.*`, `AutoBreadcrumb`) ; les imports obsolètes (`Wrapper`, `WaitingActivity`, `AnimatedContent`) sont retirés.

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
- **User-Agent standardisé** `protorians/5.0 (…) Senteints/<version>` sur toutes les requêtes HTTP de la CLI et de l'installeur npm.
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
- **create module** — Créer un module standardisé dans `library/modules/`
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
