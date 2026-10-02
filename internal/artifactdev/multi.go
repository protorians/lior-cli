// Dev-server multi-modules (mode dev, un seul port) : un processus `liora
// artifact dev` héberge plusieurs modules, chacun servi sous son slug —
// `/<slug>/index.html` — avec son propre contexte esbuild, son propre watcher
// et son propre reload SSE taggué (`reload:<slug>`).
//
// Le socle ne connaît qu'une URL de transport (`NEXT_PUBLIC_DEV_MODULES_URL`) :
// le chemin résout le module, exactement comme `/m/<slug>/…` résout l'iframe
// côté socle. La slugification est partagée avec le transport TypeScript
// (packages/sdk, `module-transport.ts`) : identifiant mis en minuscules, toute
// séquence non alphanumérique devient un tiret.
package artifactdev

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	esbuild "github.com/evanw/esbuild/pkg/api"

	"github.com/protorians/lior-cli/internal/devlink"
	"github.com/protorians/lior-cli/internal/socle"
)

// MultiOptions porte la configuration du dev-server multi-modules.
type MultiOptions struct {
	// ModuleDirs sont les racines des modules à héberger (dédupliquées).
	ModuleDirs []string
	// Dev porte les réglages partagés du serveur (port, hôte, TLS, strict).
	Dev DevOptions
}

// MultiModuleInfo décrit un module hébergé — affiché au démarrage.
type MultiModuleInfo struct {
	Identifier  string
	Slug        string
	Dir         string
	ArtifactDir string
}

// MultiDevServer est un dev-server multi-modules en cours d'exécution.
type MultiDevServer struct {
	// URL racine du serveur (ex. https://localhost:5178).
	URL string
	// Port effectif — peut différer du port demandé (bascule EADDRINUSE).
	Port int
	// HTTPS rapporte le schéma de service.
	HTTPS bool
	// PortShifted indique que le port demandé était occupé.
	PortShifted bool
	// RequestedPort est le port initialement demandé.
	RequestedPort int
	// Modules décrit les modules hébergés, dans l'ordre de déclaration.
	Modules []MultiModuleInfo

	closeOnce sync.Once
	closes    []func()
}

// Stop arrête esbuild, les watchers et le serveur HTTP(S).
func (s *MultiDevServer) Stop() {
	s.closeOnce.Do(func() {
		for i := len(s.closes) - 1; i >= 0; i-- {
			s.closes[i]()
		}
	})
}

// tenant est un module hébergé : sa configuration effective, son contexte
// esbuild et sa part de reload SSE.
type tenant struct {
	identifier   string
	slug         string
	cfg          *Config
	manifest     *Manifest
	templateData map[string]any
	ctx          esbuild.BuildContext
	rebuildMu    sync.Mutex
}

// rebuild enchaîne le registre de routes, esbuild puis le pipeline de style,
// re-rend le document hôte — le même contrat que le watch mono-module.
func (t *tenant) rebuild(log func(string)) error {
	t.rebuildMu.Lock()
	if _, err := ensureRouteRegistry(t.cfg, log); err != nil {
		t.rebuildMu.Unlock()
		return fmt.Errorf("registre de routes : %v", err)
	}
	res := t.ctx.Rebuild()
	if len(res.Errors) > 0 {
		t.rebuildMu.Unlock()
		return fmt.Errorf("%s", joinMessages(res.Errors))
	}
	if err := runStyleEngine(t.cfg, log); err != nil {
		t.rebuildMu.Unlock()
		return err
	}
	t.rebuildMu.Unlock()
	if updated, err := LoadManifest(t.cfg.ModuleDir); err == nil {
		t.manifest = updated
	}
	return renderAndWriteDocument(t.cfg, t.manifest, t.templateData)
}

// slugifyIdentifier slugifie un identifiant de module pour une route URL :
// `mod.liorian.test-1` → `mod-liorian-test-1`. Miroir exact du transport
// TypeScript du socle — les deux côtés doivent déduire le même chemin.
func slugifyIdentifier(identifier string) string {
	slug := strings.ToLower(identifier)
	slug = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(slug, "-")
	return strings.Trim(slug, "-")
}

// resolveTenants résout chaque module et construit les tenants avec leurs
// chemins de service. Chaque module est servi sous le slug de son identifiant
// canonique (la convention `/m/<slug>` du socle) et sous le slug de son nom de
// dossier — collisions rejetées, fail-closed.
func resolveTenants(moduleDirs []string, dev DevOptions, log func(string)) ([]*tenant, map[string]*tenant, error) {
	registry := map[string]*tenant{}
	tenants := make([]*tenant, 0, len(moduleDirs))
	register := func(keys []string, t *tenant) error {
		for _, key := range keys {
			if key == "" {
				continue
			}
			if existing, taken := registry[key]; taken && existing != t {
				return fmt.Errorf("artifact: collision de slug dev — %s et %s se disputent « /%s/ »",
					existing.identifier, t.identifier, key)
			}
			registry[key] = t
		}
		return nil
	}

	seen := map[string]bool{}
	for _, dir := range moduleDirs {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return nil, nil, err
		}
		if seen[abs] {
			continue
		}
		seen[abs] = true

		manifest, cfg, err := Resolve(BuildOptions{ModuleDir: abs, Dev: dev})
		if err != nil {
			return nil, nil, err
		}
		t := &tenant{
			identifier: manifest.ID,
			slug:       slugifyIdentifier(manifest.ID),
			cfg:        cfg,
			manifest:   manifest,
		}
		if t.slug == "" {
			return nil, nil, fmt.Errorf("artifact: identifiant de module non slugifiable (%s)", manifest.ID)
		}
		// Le nom de dossier est un alias de DX : `artifact dev ../crm` reste
		// atteignable sous /crm/ même quand l'identifiant canonique diverge.
		aliases := []string{t.slug, slugifyIdentifier(filepath.Base(abs))}
		if uri, _ := manifest.Extra["uri"].(string); uri != "" {
			aliases = append(aliases, slugifyIdentifier(filepath.Base(strings.Trim(uri, "/"))))
		}
		if err := register(aliases, t); err != nil {
			return nil, nil, err
		}
		tenants = append(tenants, t)
	}
	return tenants, registry, nil
}

// StartMulti démarre le dev-server multi-modules : un tenant par module
// (contexte esbuild, watcher, document hôte), un seul listen HTTP(S), un flux
// SSE global aux messages taggués par slug. Le serveur vit jusqu'à Stop.
func StartMulti(options MultiOptions, log func(string)) (*MultiDevServer, error) {
	if log == nil {
		log = func(string) {}
	}
	if len(options.ModuleDirs) == 0 {
		return nil, fmt.Errorf("artifact: aucun module à héberger")
	}

	tenants, registry, err := resolveTenants(options.ModuleDirs, options.Dev, log)
	if err != nil {
		return nil, err
	}

	// Configuration additionnelle (`artifact.config.json`) de chaque module.
	for _, t := range tenants {
		templateData, err := loadArtifactConfigJSON(t.cfg.ModuleDir)
		if err != nil {
			log(fmt.Sprintf("artifact: %v", err))
		}
		t.templateData = templateData
	}

	// Amorçage de chaque tenant : registre de routes, document hôte, wrapper,
	// contexte esbuild, rebuild initial, style, re-rendu du document avec le
	// lien CSS.
	for _, t := range tenants {
		if err := os.MkdirAll(t.cfg.ArtifactDir, 0o755); err != nil {
			return nil, err
		}
		if _, err := ensureRouteRegistry(t.cfg, log); err != nil {
			return nil, err
		}
		if err := renderAndWriteDocument(t.cfg, t.manifest, t.templateData); err != nil {
			return nil, err
		}
		wrapperPath, err := WriteBootstrapWrapper(t.cfg)
		if err != nil {
			return nil, err
		}
		ctx, ctxErr := esbuild.Context(EsbuildOptions(t.cfg, wrapperPath, ResolveWorkspaceAliases(t.cfg.ModuleDir)))
		if ctxErr != nil {
			_ = os.Remove(wrapperPath)
			return nil, fmt.Errorf("artifact: échec de l'initialisation esbuild (%s) — %v", t.identifier, ctxErr)
		}
		t.ctx = ctx
		result := ctx.Rebuild()
		if len(result.Errors) > 0 {
			log(fmt.Sprintf("artifact: échec du rebuild initial (%s) — %s", t.identifier, joinMessages(result.Errors)))
		} else {
			log(fmt.Sprintf("artifact: bundle %s (%.0f Ko) — /%s/",
				filepath.Join(t.cfg.ArtifactDir, t.cfg.Bundle), float64(bundleSize(t.cfg))/1024, t.slug))
			if err := runStyleEngine(t.cfg, log); err != nil {
				log(fmt.Sprintf("artifact: %v", err))
			}
			if err := renderAndWriteDocument(t.cfg, t.manifest, t.templateData); err != nil {
				log(fmt.Sprintf("artifact: %v", err))
			}
		}
	}

	server := &MultiDevServer{RequestedPort: options.Dev.Port}

	// Schéma : comme en mono-module, la liaison au socle tranche par défaut
	// (un socle HTTPS impose HTTPS — mixed content sinon) ; --https/--http
	// imposent. TLS est partagé : un seul serveur, un seul certificat.
	socleScheme := ""
	for _, t := range tenants {
		if link, ok := devlink.Read(t.cfg.ModuleDir); ok && link.SocleScheme != "" {
			socleScheme = link.SocleScheme
			break
		}
	}
	var tlsCertificate *tls.Config
	httpsRequested := IsTLSRequested(options.Dev.HTTPS, socleScheme)
	if httpsRequested {
		material := ResolveTLS(tenants[0].cfg.ModuleDir, options.Dev.Cert, options.Dev.Key)
		if material == nil {
			disposeTenants(tenants)
			hint := ""
			if socleScheme != "" {
				hint = fmt.Sprintf("Le socle lié est servi en %s : le dev-server doit l'être aussi. ", socleScheme)
			}
			return nil, fmt.Errorf("artifact: TLS demandé mais aucun certificat trouvé. %s%s.", hint, MissingCertsHint)
		}
		certificate, err := tls.X509KeyPair(material.Cert, material.Key)
		if err != nil {
			disposeTenants(tenants)
			return nil, fmt.Errorf("artifact: couple TLS illisible (%s) — %v", material.CertPath, err)
		}
		tlsCertificate = &tls.Config{Certificates: []tls.Certificate{certificate}}
		log(fmt.Sprintf("artifact: TLS — %s (%s)", material.CertPath, material.Source))
	}

	// Reload SSE global : un seul flux `/-/events`, messages taggués par slug
	// — un rebuild de crm ne recharge pas les iframes de billing.
	broadcast := newBroadcaster()

	// Watcher partagé : chaque changement de source déclenche le rebuild du
	// seul tenant concerné, débounce comme en mono-module.
	var timersMu sync.Mutex
	timers := map[*tenant]*time.Timer{}
	stopWatch := watchModuleDirs(tenantDirs(tenants), func(dir string) {
		var target *tenant
		for _, t := range tenants {
			if t.cfg.ModuleDir == dir {
				target = t
				break
			}
		}
		if target == nil {
			return
		}
		timersMu.Lock()
		defer timersMu.Unlock()
		if timer := timers[target]; timer != nil {
			timer.Stop()
		}
		timers[target] = time.AfterFunc(80*time.Millisecond, func() {
			if err := target.rebuild(log); err != nil {
				log(fmt.Sprintf("artifact: échec du rebuild (%s) — %v", target.identifier, err))
				return
			}
			broadcast.send("reload:" + target.slug)
		})
	})
	server.closes = append(server.closes, stopWatch, func() { disposeTenants(tenants) })

	handler := newMultiHandler(registry, broadcast)
	listeners, port, shifted, listenErr := listenWithFallback(options.Dev.Host, options.Dev.Port, options.Dev.StrictPort, log)
	if listenErr != nil {
		server.Stop()
		return nil, listenErr
	}

	for _, ln := range listeners {
		httpServer := &http.Server{Handler: handler, TLSConfig: tlsCertificate, ReadHeaderTimeout: 10 * time.Second}
		ln := ln
		go func() {
			var serveErr error
			if tlsCertificate != nil {
				serveErr = httpServer.ServeTLS(ln, "", "")
			} else {
				serveErr = httpServer.Serve(ln)
			}
			if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
				log(fmt.Sprintf("artifact: serveur arrêté — %v", serveErr))
			}
		}()
		server.closes = append(server.closes, func() { _ = httpServer.Close() })
	}

	server.Port = port
	server.PortShifted = shifted
	server.HTTPS = tlsCertificate != nil
	scheme := "http"
	if server.HTTPS {
		scheme = "https"
	}
	server.URL = fmt.Sprintf("%s://%s:%d", scheme, options.Dev.Host, port)
	for _, t := range tenants {
		server.Modules = append(server.Modules, MultiModuleInfo{
			Identifier:  t.identifier,
			Slug:        t.slug,
			Dir:         t.cfg.ModuleDir,
			ArtifactDir: t.cfg.ArtifactDir,
		})
	}

	if shifted {
		log(fmt.Sprintf("artifact: port %d occupé (EADDRINUSE) — dev-server basculé sur %d", options.Dev.Port, port))
	}

	// Câblage du socle : chaque module déjà lié (devlink) voit son `.env.local`
	// mis au niveau du lot — URL unique, lot d'identifiants, mode multi. C'est
	// ce qui rend le lot visible du socle sans repasser par `bind:socle`.
	for _, t := range tenants {
		link, ok := devlink.Read(t.cfg.ModuleDir)
		if !ok {
			continue
		}
		if link.DevPort != port {
			link.DevPort = port
			if _, err := devlink.Write(t.cfg.ModuleDir, link); err == nil {
				log(fmt.Sprintf("artifact: %s réaligné sur le port %d (%s)", devlink.File, port, t.identifier))
			}
		}
		for _, change := range []struct{ key, value string }{
			{socle.KeyDevModulesURL, server.URL},
			{socle.KeyDevModulesMulti, "1"},
		} {
			if changed, err := socle.EnsureEnvLocalKey(link.SocleDir, change.key, change.value); err == nil && changed {
				log(fmt.Sprintf("artifact: %s mis à jour dans %s", change.key, filepath.Join(link.SocleDir, socle.EnvLocalFile)))
			}
		}
		if changed, err := socle.EnsureEnvLocalDevModules(link.SocleDir, t.identifier); err == nil && changed {
			log(fmt.Sprintf("artifact: NEXT_PUBLIC_DEV_MODULES inclut désormais %s", t.identifier))
		}
	}

	if !server.HTTPS && socleScheme == "https" {
		log("artifact: socle en HTTPS, dev-server en HTTP — le navigateur bloquera l'iframe (mixed content). " +
			"Lance avec `--https`, ou installe les certificats du socle : " + MissingCertsHint + ".")
	}
	log(fmt.Sprintf("artifact: dev-server multi-modules en écoute sur %s", server.URL))
	return server, nil
}

// disposeTenants dispose tous les contextes esbuild (arrêt propre, y compris
// sur les chemins d'erreur avant l'écoute).
func disposeTenants(tenants []*tenant) {
	for _, t := range tenants {
		if t.ctx != nil {
			t.ctx.Dispose()
		}
	}
}

func tenantDirs(tenants []*tenant) []string {
	dirs := make([]string, 0, len(tenants))
	for _, t := range tenants {
		dirs = append(dirs, t.cfg.ModuleDir)
	}
	return dirs
}

// multiReloadScript injecte le flux SSE dans le document hôte d'un tenant :
// le reload taggué de son slug, plus le message nu des serveurs legacy.
func multiReloadScript(slug string) string {
	return `<script>` +
		`new EventSource('/-/events').onmessage = function (event) {` +
		`  if (event.data === 'reload' || event.data === 'reload:` + slug + `') location.reload();` +
		`};` +
		`</script>`
}

// newMultiHandler construit le handler multi-tenant : SSE global, page racine
// d'annuaire, service statique préfixé par slug ou alias (repli SPA vers **le**
// document hôte du tenant — jamais vers celui d'un autre module).
func newMultiHandler(registry map[string]*tenant, broadcast *broadcaster) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path == "/-/events" {
			serveEvents(w, r, broadcast)
			return
		}

		segments := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 2)
		target, ok := registry[segments[0]]
		if !ok {
			writeModuleIndex(w, registry)
			return
		}
		rest := "/"
		if len(segments) == 2 && segments[1] != "" {
			rest = "/" + segments[1]
		}
		scoped := r.Clone(r.Context())
		scoped.URL.Path = rest
		serveArtifact(w, scoped, target.cfg, func(html string) string {
			return injectScript(html, multiReloadScript(target.slug))
		})
	})
}

// writeModuleIndex rend l'annuaire des modules hébergés à la racine du
// serveur : la page la plus utile quand on ouvre l'URL sans chemin.
func writeModuleIndex(w http.ResponseWriter, registry map[string]*tenant) {
	slugs := make([]string, 0, len(registry))
	seen := map[string]bool{}
	for slug, t := range registry {
		if seen[slug] || t.slug != slug {
			continue
		}
		seen[slug] = true
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	var body strings.Builder
	body.WriteString("<!doctype html><html><head><meta charset=\"utf-8\" /><title>liora artifact dev</title></head><body><h1>Modules hébergés</h1><ul>")
	for _, slug := range slugs {
		fmt.Fprintf(&body, `<li><a href="/%s/index.html">%s</a></li>`, slug, slug)
	}
	body.WriteString("</ul></body></html>")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(body.String()))
}
