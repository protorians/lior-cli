// Dev-server d'un module (spec §8, mode dev — D11 : `modules/<id>/` est la
// source, `library/` la destination d'installation ; on ne développe jamais
// dans la bibliothèque).
//
// Le watcher d'esbuild reconstruit le bundle à chaque changement de source,
// un serveur HTTP(S) sert `.liorian/artifact/` avec
// `Access-Control-Allow-Origin: *` (l'iframe du module est d'origine opaque)
// et un flux SSE (`/-/events`) déclenche le rechargement du document hôte
// après chaque rebuild réussi.
//
// Le socle pointe l'iframe du module vers ce serveur via
// `NEXT_PUBLIC_DEV_MODULES_URL` — le port doit être publié sur l'hôte quand
// le socle tourne sous docker (l'iframe est chargée par le navigateur). Le
// serveur est servi dans le même contexte que le socle : un socle de
// développement est en HTTPS et bloquerait une iframe `http://` en mixed
// content (cf. tls.go).
//
// Gestion du port : le port demandé est sondé avant le service et, s'il est
// occupé (EADDRINUSE — typiquement un dev-server orphelin qui survit à un
// terminal fermé sans SIGINT, ou un docker-proxy), le serveur bascule sur le
// port suivant avec un message explicite, sans jamais démarrer à moitié. Le
// lien de développement (`.liorian/dev.json`) est réaligné pour que `liora
// doctor` et le transport voient le port réel.
package artifactdev

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	esbuild "github.com/evanw/esbuild/pkg/api"

	"github.com/protorians/lior-cli/internal/devlink"
)

// MIME des fichiers servis (bundle, document, assets du module).
var mimeTypes = map[string]string{
	".html":  "text/html; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".mjs":   "text/javascript; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".json":  "application/json; charset=utf-8",
	".map":   "application/json; charset=utf-8",
	".svg":   "image/svg+xml",
	".png":   "image/png",
	".jpg":   "image/jpeg",
	".jpeg":  "image/jpeg",
	".gif":   "image/gif",
	".webp":  "image/webp",
	".avif":  "image/avif",
	".bmp":   "image/bmp",
	".ico":   "image/x-icon",
	".wasm":  "application/wasm",
	".woff":  "font/woff",
	".woff2": "font/woff2",
	".ttf":   "font/ttf",
	".otf":   "font/otf",
	".eot":   "application/vnd.ms-fontobject",
	".mp4":   "video/mp4",
	".webm":  "video/webm",
	".mov":   "video/quicktime",
	".m4v":   "video/x-m4v",
	".mp3":   "audio/mpeg",
	".wav":   "audio/wav",
	".ogg":   "audio/ogg",
	".oga":   "audio/ogg",
	".m4a":   "audio/mp4",
	".aac":   "audio/aac",
	".flac":  "audio/flac",
	".txt":   "text/plain; charset=utf-8",
}

// reloadScript injecte le flux SSE dans le document hôte servi.
const reloadScript = `<script>` +
	`new EventSource('/-/events').onmessage = function (event) {` +
	`  if (event.data === 'reload') location.reload();` +
	`};` +
	`</script>`

// DevServer est un dev-server en cours d'exécution.
type DevServer struct {
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

	closeOnce sync.Once
	closes    []func()
}

// Stop arrête esbuild, le watcher de template et les serveurs HTTP(S).
func (s *DevServer) Stop() {
	s.closeOnce.Do(func() {
		for i := len(s.closes) - 1; i >= 0; i-- {
			s.closes[i]()
		}
	})
}

// broadcaster diffuse le reload SSE à toutes les iframes connectées.
type broadcaster struct {
	mu      sync.Mutex
	clients map[chan string]struct{}
}

func newBroadcaster() *broadcaster {
	return &broadcaster{clients: make(map[chan string]struct{})}
}

func (b *broadcaster) subscribe() chan string {
	ch := make(chan string, 8)
	b.mu.Lock()
	b.clients[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *broadcaster) unsubscribe(ch chan string) {
	b.mu.Lock()
	delete(b.clients, ch)
	b.mu.Unlock()
}

func (b *broadcaster) send(message string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.clients {
		select {
		case ch <- message:
		default:
			// Client trop lent : il ratera ce reload, le suivant le rattrapera.
		}
	}
}

// Start démarre le dev-server : wrapper, build esbuild initial, watcher,
// document hôte, TLS et écoute. Le serveur vit jusqu'à Stop.
func Start(options BuildOptions, log func(string)) (*DevServer, error) {
	if log == nil {
		log = func(string) {}
	}
	manifest, cfg, err := Resolve(options)
	if err != nil {
		return nil, err
	}

	// Configuration additionnelle du module (`artifact.config.json`).
	templateData, err := loadArtifactConfigJSON(cfg.ModuleDir)
	if err != nil {
		log(fmt.Sprintf("artifact: %v", err))
	}

	broadcast := newBroadcaster()

	// Document hôte rendu avant le service : un module fraîchement scaffoldé
	// peut développer sans passer par un build préalable.
	if err := renderAndWriteDocument(cfg, manifest, templateData); err != nil {
		return nil, err
	}

	// Bundle esbuild : contexte persistant, rebuild initial, puis watch —
	// esbuild surveille lui-même le graphe de l'entrée (wrapper inclus).
	wrapperPath, err := WriteBootstrapWrapper(cfg)
	if err != nil {
		return nil, err
	}
	ctx, ctxErr := esbuild.Context(EsbuildOptions(cfg, wrapperPath, ResolveWorkspaceAliases(cfg.ModuleDir)))
	if ctxErr != nil {
		_ = os.Remove(wrapperPath)
		return nil, fmt.Errorf("artifact: échec de l'initialisation esbuild — %v", ctxErr)
	}
	result := ctx.Rebuild()
	if len(result.Errors) > 0 {
		log("artifact: échec du rebuild initial — " + joinMessages(result.Errors))
	} else {
		log(fmt.Sprintf("artifact: bundle %s (%.0f Ko)", filepath.Join(cfg.ArtifactDir, cfg.Bundle),
			float64(bundleSize(cfg))/1024))
	}

	server := &DevServer{RequestedPort: cfg.Dev.Port}

	// Watcher maison : esbuild v0.28 n'expose pas de callback de watch — le
	// sondage léger de l'arbre du module déclenche le rebuild incrémental du
	// contexte, recharge le manifeste, re-rend le document hôte (template,
	// config) et diffuse le reload. Débounce court contre les rafales
	// d'éditeurs ; chaque événement réarme le timer, aucune modification n'est
	// perdue.
	var rebuildMu sync.Mutex
	var timerMu sync.Mutex
	var timer *time.Timer
	scheduleRebuild := func() {
		timerMu.Lock()
		defer timerMu.Unlock()
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(80*time.Millisecond, func() {
			rebuildMu.Lock()
			res := ctx.Rebuild()
			rebuildMu.Unlock()
			if len(res.Errors) > 0 {
				log("artifact: échec du rebuild — " + joinMessages(res.Errors))
				return
			}
			if updated, err := LoadManifest(cfg.ModuleDir); err == nil {
				manifest = updated
			}
			if err := renderAndWriteDocument(cfg, manifest, templateData); err != nil {
				log(fmt.Sprintf("artifact: %v", err))
				return
			}
			broadcast.send("reload")
		})
	}
	stopWatch := watchModuleSources(cfg, scheduleRebuild)
	server.closes = append(server.closes, stopWatch, ctx.Dispose)

	// Schéma : la liaison au socle tranche par défaut (un socle HTTPS impose
	// HTTPS au module — mixed content sinon) ; --https/--http imposent.
	socleScheme := ""
	if link, ok := devlink.Read(cfg.ModuleDir); ok {
		socleScheme = link.SocleScheme
	}
	httpsWanted := IsTLSRequested(cfg.Dev.HTTPS, socleScheme)
	var tlsConfig *tls.Config
	if httpsWanted {
		material := ResolveTLS(cfg.ModuleDir, cfg.Dev.Cert, cfg.Dev.Key)
		if material == nil {
			ctx.Dispose()
			_ = os.Remove(wrapperPath)
			stopWatch()
			hint := ""
			if socleScheme != "" {
				hint = fmt.Sprintf("Le socle lié est servi en %s : le module doit l'être aussi. ", socleScheme)
			}
			return nil, fmt.Errorf("artifact: TLS demandé mais aucun certificat trouvé. %s%s.", hint, MissingCertsHint)
		}
		certificate, err := tls.X509KeyPair(material.Cert, material.Key)
		if err != nil {
			ctx.Dispose()
			_ = os.Remove(wrapperPath)
			stopWatch()
			return nil, fmt.Errorf("artifact: couple TLS illisible (%s) — %v", material.CertPath, err)
		}
		tlsConfig = &tls.Config{Certificates: []tls.Certificate{certificate}}
		log(fmt.Sprintf("artifact: TLS — %s (%s)", material.CertPath, material.Source))
	}

	handler := newHandler(cfg, broadcast)
	listeners, port, shifted, listenErr := listenWithFallback(cfg.Dev.Host, cfg.Dev.Port, cfg.Dev.StrictPort, log)
	if listenErr != nil {
		ctx.Dispose()
		_ = os.Remove(wrapperPath)
		stopWatch()
		return nil, listenErr
	}

	for _, ln := range listeners {
		httpServer := &http.Server{Handler: handler, TLSConfig: tlsConfig, ReadHeaderTimeout: 10 * time.Second}
		serve := httpServer
		ln := ln
		go func() {
			var serveErr error
			if tlsConfig != nil {
				serveErr = serve.ServeTLS(ln, "", "")
			} else {
				serveErr = serve.Serve(ln)
			}
			if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
				log(fmt.Sprintf("artifact: serveur arrêté — %v", serveErr))
			}
		}()
		server.closes = append(server.closes, func() { _ = serve.Close() })
	}

	server.Port = port
	server.PortShifted = shifted
	server.HTTPS = tlsConfig != nil
	scheme := "http"
	if server.HTTPS {
		scheme = "https"
	}
	server.URL = fmt.Sprintf("%s://%s:%d", scheme, cfg.Dev.Host, port)

	if shifted {
		log(fmt.Sprintf("artifact: port %d occupé (EADDRINUSE) — dev-server basculé sur %d", cfg.Dev.Port, port))
		// Le lien de développement est réaligné : `liora doctor` et le
		// transport du socle doivent voir le port réel.
		if link, ok := devlink.Read(cfg.ModuleDir); ok && link.DevPort != port {
			link.DevPort = port
			if _, err := devlink.Write(cfg.ModuleDir, link); err == nil {
				log(fmt.Sprintf(
					"artifact: %s réaligné sur le port %d — relance `liora artifact bind:socle` (ou mets à jour "+
						"NEXT_PUBLIC_DEV_MODULES_URL) et redémarre le socle pour que l'iframe suive",
					devlink.File, port))
			}
		}
	}
	log(fmt.Sprintf("artifact: dev-server en écoute sur %s (artefact : %s)", server.URL, cfg.ArtifactDir))
	if !server.HTTPS && socleScheme == "https" {
		log("artifact: socle en HTTPS, dev-server en HTTP — le navigateur bloquera l'iframe (mixed content). " +
			"Lance avec `--https`, ou installe les certificats du socle : " + MissingCertsHint + ".")
	}
	if len(result.Errors) > 0 {
		log("artifact: le dernier rebuild est en échec — corrige la source, le reload reprendra")
	}
	return server, nil
}

// listenWithFallback sonde le port demandé puis les suivants (jusqu'à
// PortFallbackAttempts). Sur l'interface loopback (`localhost`), les deux
// piles sont sondées ensemble — IPv4 et IPv6 — pour qu'un process orphelin
// qui ne tient que [::1]:5178 déclenche bien la bascule, et que le port servi
// soit le même quel que soit le choix de résolution du navigateur.
func listenWithFallback(host string, port int, strict bool, log func(string)) ([]net.Listener, int, bool, error) {
	if port <= 0 {
		return nil, 0, false, fmt.Errorf("artifact: port invalide (%d)", port)
	}
	// `localhost` est servi sur les deux piles de loopback — IPv4 et IPv6 :
	// un navigateur qui résout localhost vers ::1 (macOS le fait en premier)
	// et un process orphelin qui ne tient que [::1] doivent tous deux être
	// gérés.
	loopback := host == "localhost"

	for attempt := 0; attempt <= PortFallbackAttempts; attempt++ {
		candidate := port + attempt
		var listeners []net.Listener

		primary, primaryErr := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(candidate)))
		if loopback {
			if primaryErr != nil {
				if isAddrInUse(primaryErr) {
					if strict {
						return nil, 0, false, primaryErr
					}
					log(portBusyLog(host, candidate, primaryErr))
					continue
				}
				return nil, 0, false, primaryErr
			}
			listeners = append(listeners, primary)

			// IPv6 loopback : nécessaire dès qu'un client résout localhost
			// vers ::1 (macOS le fait en premier). Absent du système → on
			// sert en IPv4 seul ; occupé → le port est déjà pris.
			secondary, secondaryErr := net.Listen("tcp6", net.JoinHostPort("::1", strconv.Itoa(candidate)))
			if secondaryErr == nil {
				listeners = append(listeners, secondary)
			} else if isAddrInUse(secondaryErr) {
				_ = primary.Close()
				if strict {
					return nil, 0, false, secondaryErr
				}
				log(portBusyLog(host, candidate, secondaryErr))
				continue
			} else {
				log(fmt.Sprintf("artifact: loopback IPv6 indisponible (%v) — service en IPv4 seul", secondaryErr))
			}
		} else {
			if primaryErr == nil {
				listeners = append(listeners, primary)
			} else if isAddrInUse(primaryErr) && !strict {
				log(portBusyLog(host, candidate, primaryErr))
				continue
			} else {
				// Interface non loopback : on tente la résolution générique.
				generic, genericErr := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(candidate)))
				if genericErr != nil {
					if isAddrInUse(genericErr) && !strict {
						log(portBusyLog(host, candidate, genericErr))
						continue
					}
					return nil, 0, false, genericErr
				}
				listeners = append(listeners, generic)
			}
		}
		return listeners, candidate, attempt > 0, nil
	}
	return nil, 0, false, fmt.Errorf(
		"artifact: aucun port libre de %d à %d sur %s — ferme les dev-servers résiduels (lsof -i :%d)",
		port, port+PortFallbackAttempts, host, port)
}

func portBusyLog(host string, port int, err error) string {
	return fmt.Sprintf("artifact: %s occupé (%v) — essai du port suivant", net.JoinHostPort(host, strconv.Itoa(port)), err)
}

// isAddrInUse rapporte une erreur d'occupation de port (EADDRINUSE/EADDRNOTAVAIL).
func isAddrInUse(err error) bool {
	var syscallErr *os.SyscallError
	if errors.As(err, &syscallErr) {
		return errors.Is(syscallErr.Err, syscall.EADDRINUSE) || errors.Is(syscallErr.Err, syscall.EADDRNOTAVAIL)
	}
	return false
}

// newHandler construit le handler HTTP : SSE pour /-/events, service statique
// du répertoire d'artefact avec repli SPA sur le document hôte.
func newHandler(cfg *Config, broadcast *broadcaster) http.Handler {
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

		relative := strings.TrimPrefix(r.URL.Path, "/")
		if relative == "" {
			relative = cfg.Document
		}
		filePath := filepath.Join(cfg.ArtifactDir, filepath.FromSlash(relative))
		if !strings.HasPrefix(filePath, cfg.ArtifactDir+string(filepath.Separator)) && filePath != cfg.ArtifactDir {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		info, err := os.Stat(filePath)
		if err != nil || info.IsDir() {
			// Repli SPA : toute route inconnue sert le document hôte (§4.3).
			filePath = filepath.Join(cfg.ArtifactDir, cfg.Document)
			info, err = os.Stat(filePath)
			if err != nil {
				w.WriteHeader(http.StatusNotFound)
				return
			}
		}

		mime := mimeTypes[strings.ToLower(filepath.Ext(filePath))]
		if mime == "" {
			mime = "application/octet-stream"
		}
		w.Header().Set("Content-Type", mime)
		if strings.HasPrefix(mime, "text/html") {
			content, err := os.ReadFile(filePath)
			if err != nil {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			html := injectReloadScript(string(content))
			w.Header().Set("Content-Length", strconv.Itoa(len(html)))
			if r.Method == http.MethodHead {
				return
			}
			_, _ = w.Write([]byte(html))
			return
		}
		w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
		if r.Method == http.MethodHead {
			return
		}
		http.ServeFile(w, r, filePath)
	})
}

// serveEvents maintient le flux SSE du reload.
func serveEvents(w http.ResponseWriter, r *http.Request, broadcast *broadcaster) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(": connecté\n\n"))
	flusher.Flush()

	events := broadcast.subscribe()
	defer broadcast.unsubscribe(events)
	notify := r.Context().Done()
	for {
		select {
		case <-notify:
			return
		case message := <-events:
			if _, err := w.Write([]byte("data: " + message + "\n\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// injectReloadScript insère le script de rechargement avant la fermeture du
// body (ou en queue quand le template n'a pas de </body>).
func injectReloadScript(html string) string {
	if strings.Contains(html, "</body>") {
		return strings.Replace(html, "</body>", reloadScript+"\n</body>", 1)
	}
	return html + reloadScript
}

// renderAndWriteDocument rend le document hôte et l'écrit dans le répertoire
// d'artefact.
func renderAndWriteDocument(cfg *Config, manifest *Manifest, templateData map[string]any) error {
	document, err := RenderHostDocument(cfg, manifest, templateData)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.ArtifactDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(cfg.ArtifactDir, cfg.Document), []byte(document), 0o644)
}

// moduleWatchSkip sont les répertoires exclus du sondage du module : les
// dépendances, l'état de build (l'artefact servi n'est pas une source) et le
// VCS.
var moduleWatchSkip = map[string]bool{
	"node_modules": true,
	".liorian":     true,
	".git":         true,
	"artifact":     true,
}

// watchModuleSources surveille l'arbre du module — par sondage léger, sans
// dépendance — et appelle onChange à chaque modification détectée (sources,
// template du document hôte, manifeste, artifact.config.json). Le sondage
// relève taille + mtime de chaque fichier régulier ; une signature stable
// entre deux passages n'appelle rien.
func watchModuleSources(cfg *Config, onChange func()) func() {
	type fileState struct {
		size    int64
		modNano int64
	}
	signature := func() map[string]fileState {
		state := map[string]fileState{}
		_ = filepath.Walk(cfg.ModuleDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				if moduleWatchSkip[info.Name()] && path != cfg.ModuleDir {
					return filepath.SkipDir
				}
				return nil
			}
			if !info.Mode().IsRegular() || strings.HasPrefix(info.Name(), ".") {
				return nil
			}
			state[path] = fileState{size: info.Size(), modNano: info.ModTime().UnixNano()}
			return nil
		})
		return state
	}
	same := func(a, b map[string]fileState) bool {
		if len(a) != len(b) {
			return false
		}
		for path, stateA := range a {
			if stateB, ok := b[path]; !ok || stateB != stateA {
				return false
			}
		}
		return true
	}
	previous := signature()
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(400 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				current := signature()
				if same(current, previous) {
					continue
				}
				previous = current
				onChange()
			}
		}
	}()
	return func() { close(done) }
}

// loadArtifactConfigJSON lit `artifact.config.json` (templateData uniquement).
// Les surcharges TS (`artifact.config.ts`) exigent un évaluateur JavaScript :
// elles sont signalées, jamais silencieusement ignorées.
func loadArtifactConfigJSON(moduleDir string) (map[string]any, error) {
	for _, name := range []string{"artifact.config.ts", "artifact.config.mts", "artifact.config.js", "artifact.config.mjs"} {
		if _, err := os.Stat(filepath.Join(moduleDir, name)); err == nil {
			return nil, fmt.Errorf("%s ignoré — la CLI native ne lit que artifact.config.json", name)
		}
	}
	path := filepath.Join(moduleDir, "artifact.config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]any{}, nil
	}
	var config struct {
		TemplateData map[string]any `json:"templateData"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("artifact.config.json illisible : %v", err)
	}
	if config.TemplateData == nil {
		config.TemplateData = map[string]any{}
	}
	return config.TemplateData, nil
}

func joinMessages(messages []esbuild.Message) string {
	texts := make([]string, 0, len(messages))
	for _, message := range messages {
		texts = append(texts, message.Text)
	}
	return strings.Join(texts, "; ")
}

func bundleSize(cfg *Config) int64 {
	info, err := os.Stat(filepath.Join(cfg.ArtifactDir, cfg.Bundle))
	if err != nil {
		return 0
	}
	return info.Size()
}
