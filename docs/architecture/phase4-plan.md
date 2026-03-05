# Phase 4: HTTP-Middleware-Stack + Server — Implementierungsplan

**Status**: Planung abgeschlossen
**Datum**: 2026-03-03
**Abhaengigkeit**: Phase 2 (Config + Models)
**Geschaetzte Dauer**: 3 Tage

---

## 1. Analyse der Python-Referenzimplementierung

### 1.1 Flask-App-Initialisierung (`web/__init__.py`, Zeile 67-84)

Die Python-Implementierung verwendet:

- `Flask(__name__)` mit `static_folder="static"` und `template_folder="static"`
- `app.config.from_prefixed_env()` laedt `FLASK_*` Umgebungsvariablen
- `app.secret_key` wird aus `FLASK_SECRET_KEY` geladen oder als zufaelliger Token generiert
- Persistenter Secret-Key in `~/.config/ankerctl/flask_secret.key` (Zeile 1269-1275)
- `ServiceManager()` wird an `app.svc` angehaengt
- `MAX_CONTENT_LENGTH` aus `UPLOAD_MAX_MB` (Default: 2048 MB)
- Session-Cookie: `SameSite=Strict`, `HttpOnly=True`

### 1.2 Middleware / Before-Request-Handler

Flask nutzt `@app.before_request`-Dekoratoren als Middleware-Equivalent. Die Python-Implementierung hat **drei** before_request-Handler und **einen** after_request-Handler:

#### 1.2.1 `_require_printer_for_control()` (Zeile 1480-1489)

- Gibt 503 fuer `/api/printer/*`, `/api/files/*`, `/api/filaments*` zurueck wenn kein Drucker konfiguriert ist
- `/static/*` wird immer durchgelassen
- Prueft: `app.config["login"]` ist `False`

#### 1.2.2 `_block_unsupported_device()` (Zeile 1492-1514)

- Gibt 503 fuer Drucker-Kontroll-Pfade zurueck wenn `unsupported_device=True`
- Gleiche Prefix-Liste: `/api/printer/`, `/api/files/`, `/api/filaments`
- `/static/*` immer erlaubt

#### 1.2.3 `_check_api_key()` (Zeile 1517-1570)

**Kernlogik der Auth-Middleware — MUSS exakt portiert werden:**

1. Kein API-Key konfiguriert → alle Requests erlaubt (abwaertskompatibel)
2. `/static/*` → immer erlaubt (kein Auth)
3. `?apikey=` URL-Parameter → Session-Cookie setzen + Redirect ohne apikey-Parameter
4. `X-Api-Key` Header → erlaubt
5. GET/HEAD/OPTIONS → erlaubt, **AUSSER** Pfad ist in `_PROTECTED_GET_PATHS` oder startet mit `/api/debug/`
6. Setup-Pfade (`/api/ankerctl/config/upload`, `/api/ankerctl/config/login`) → erlaubt wenn kein Drucker konfiguriert
7. Session-Cookie `authenticated=True` → erlaubt
8. Sonst → 401 Unauthorized

**`_PROTECTED_GET_PATHS`** (Zeile 1448-1460):
```
/api/ankerctl/server/reload
/api/debug/state
/api/debug/logs
/api/debug/services
/api/settings/mqtt
/api/notifications/settings
/api/printers
/api/history
```

**`_SETUP_PATHS`** (Zeile 1463-1466):
```
/api/ankerctl/config/upload
/api/ankerctl/config/login
```

**Printer-Control-Prefixes** (Zeile 1473-1477):
```
/api/printer/
/api/files/
/api/filaments
```

#### 1.2.4 `add_security_headers()` (Zeile 1436-1443, after_request)

```
X-Content-Type-Options: nosniff
X-Frame-Options: SAMEORIGIN
Referrer-Policy: strict-origin-when-cross-origin
Server: ankerctl
```

### 1.3 Server-Start (`webserver()`, Zeile 1248-1325)

Initialisierungsschritte:
1. FilamentStore oeffnen (`config_root / "filament.db"`)
2. Flask Secret-Key persistieren (wenn kein `FLASK_SECRET_KEY`)
3. API-Key aus ENV oder Config auflösen
4. `PRINTER_INDEX` Locking: wenn ENV gesetzt, ist Druckerwechsel blockiert
5. Aktiven Drucker aus Config laden (Fallback auf `active_printer_index`)
6. Video-Support prüfen (Model nicht in `PRINTERS_WITHOUT_CAMERA`)
7. Unsupported-Device-Check (`UNSUPPORTED_PRINTERS`)
8. Services registrieren (wenn cfg vorhanden und nicht unsupported)
9. Context-Processor fuer `debug_mode` Template-Variable
10. `app.run(host, port)`

### 1.4 Was in Phase 4 NICHT implementiert wird

- Alle Route-Handler (Phase 10)
- WebSocket-Handler (Phase 11)
- Service-Registrierung und -Management (Phase 8/9)
- FilamentStore (Phase 5)
- Template-Rendering (Phase 14)
- Tatsaechliche Config-Logik (Phase 2 liefert nur Lesen/Schreiben)

---

## 2. Detaillierter Implementierungsplan

### 2.1 `internal/web/server.go` — HTTP-Server-Setup

**Verantwortung**: chi-Router erstellen, Middleware-Kette aufbauen, Server starten und stoppen.

```go
package web

// Server haelt den HTTP-Server-Zustand
type Server struct {
    router     chi.Router
    httpServer *http.Server
    config     *config.Manager    // Phase 2 ConfigManager
    logger     *slog.Logger

    // Runtime-Zustand (entspricht app.config[] in Python)
    apiKey           string
    login            bool           // true wenn Drucker konfiguriert
    printerIndex     int
    printerIndexLocked bool
    videoSupported   bool
    unsupportedDevice bool
    host             string
    port             int

    mu sync.RWMutex  // Schuetzt Runtime-Zustand
}

// NewServer erstellt einen neuen Server mit vollstaendigem Middleware-Stack
func NewServer(cfg *config.Manager, opts ...Option) *Server

// Option-Pattern fuer Server-Konfiguration
type Option func(*Server)
func WithHost(host string) Option
func WithPort(port int) Option
func WithAPIKey(key string) Option
func WithInsecure(insecure bool) Option

// Start startet den HTTP-Server (blockierend)
func (s *Server) Start(ctx context.Context) error

// Shutdown faehrt den Server graceful herunter
func (s *Server) Shutdown(ctx context.Context) error
```

**Middleware-Kette (in chi.Use-Reihenfolge)**:
```go
r := chi.NewRouter()

// 1. Recovery — faengt Panics ab
r.Use(middleware.Recoverer)  // chi built-in

// 2. Request-ID
r.Use(middleware.RequestID)  // chi built-in

// 3. Access-Logging (custom)
r.Use(mw.AccessLogger(logger))

// 4. Security-Headers (custom)
r.Use(mw.SecurityHeaders)

// 5. Rate-Limiting (custom, IP-basiert)
r.Use(mw.RateLimit(100, time.Minute))  // 100 req/min pro IP

// 6. Body-Size-Limit
r.Use(mw.BodySizeLimit(maxUploadBytes))

// 7a. Printer-Required-Check (custom, vor Auth)
r.Use(mw.RequirePrinter(s))

// 7b. Unsupported-Device-Check (custom, vor Auth)
r.Use(mw.BlockUnsupportedDevice(s))

// 7c. Auth-Middleware (custom)
r.Use(mw.Auth(s))
```

**Akzeptanzkriterien**:
- [ ] Server startet und hoert auf konfiguriertem Host:Port
- [ ] Alle 7+ Middleware in korrekter Reihenfolge registriert
- [ ] Graceful Shutdown mit context.Context
- [ ] Runtime-Zustand thread-safe (sync.RWMutex)
- [ ] Env-Variablen: `FLASK_HOST`, `FLASK_PORT`, `UPLOAD_MAX_MB` werden gelesen
- [ ] Default: `127.0.0.1:4470`
- [ ] Server-Struct exportiert Getter fuer Runtime-Zustand (Login, PrinterIndex etc.)
- [ ] Kein Panic auf Production-Pfaden

### 2.2 `internal/web/middleware/auth.go` — API-Key-Authentifizierung

**Kernlogik** (exakt wie Python `_check_api_key`):

```go
package middleware

// Auth erstellt die API-Key-Auth-Middleware
func Auth(state AuthState) func(http.Handler) http.Handler

// AuthState ist ein Interface fuer den Server-Zustand den Auth braucht
type AuthState interface {
    APIKey() string
    IsLoggedIn() bool
}
```

**Pruefungsreihenfolge** (MUSS exakt sein):

1. `APIKey() == ""` → next (kein Key konfiguriert)
2. Path startet mit `/static/` → next
3. `?apikey=` Parameter matcht → Session setzen, Redirect ohne apikey
4. `X-Api-Key` Header matcht → next
5. Methode ist GET/HEAD/OPTIONS UND Pfad NICHT in protected-Set UND Pfad startet NICHT mit `/api/debug/` → next
6. Pfad in Setup-Paths UND `!IsLoggedIn()` → next
7. Session-Cookie `authenticated=true` → next
8. → 401 Unauthorized JSON

**Session-Handling**: In Go gibt es kein Flask-Session-Equivalent. Zwei Optionen:
- **Option A (empfohlen)**: Signierter Cookie mit `gorilla/securecookie` oder `crypto/hmac`
- **Option B**: In-Memory-Session-Map (kein Persistence ueber Restart)

Fuer Phase 4 verwenden wir **Option A** mit HMAC-signierten Cookies. Der Secret-Key wird entweder aus `FLASK_SECRET_KEY` gelesen oder persistent in `flask_secret.key` gespeichert (wie Python).

**Datenstrukturen fuer Auth-Regeln**:

```go
var protectedGETPaths = map[string]bool{
    "/api/ankerctl/server/reload": true,
    "/api/debug/state":            true,
    "/api/debug/logs":             true,
    "/api/debug/services":         true,
    "/api/settings/mqtt":          true,
    "/api/notifications/settings": true,
    "/api/printers":               true,
    "/api/history":                true,
}

var setupPaths = map[string]bool{
    "/api/ankerctl/config/upload": true,
    "/api/ankerctl/config/login":  true,
}
```

**Wichtig**: `/api/debug/logs/<filename>` und `/api/debug/services/<name>/restart` etc. werden durch den Prefix-Check `/api/debug/` abgedeckt, nicht durch die protectedGETPaths-Map.

**Akzeptanzkriterien**:
- [ ] Kein API-Key → alle Requests passieren (abwaertskompatibel)
- [ ] POST/DELETE immer Auth-pflichtig (ausser Setup-Paths ohne Login)
- [ ] GET standardmaessig offen, ausser Protected-Paths und /api/debug/*
- [ ] `?apikey=` setzt Cookie UND macht Redirect (apikey aus URL entfernen)
- [ ] `X-Api-Key` Header wird geprueft
- [ ] Session-Cookie wird geprueft
- [ ] Setup-Paths ohne Auth wenn kein Drucker konfiguriert
- [ ] 401 Response ist JSON: `{"error": "Unauthorized. Provide API key..."}`
- [ ] `/static/*` immer ohne Auth

### 2.3 `internal/web/middleware/security.go` — Security-Headers

```go
package middleware

// SecurityHeaders fuegt Security-Header zu jeder Response hinzu
func SecurityHeaders(next http.Handler) http.Handler
```

**Header (exakt wie Python)**:
```
X-Content-Type-Options: nosniff
X-Frame-Options: SAMEORIGIN
Referrer-Policy: strict-origin-when-cross-origin
Server: ankerctl
```

**Zusaetzlich (nicht in Python, aber Best Practice)**:
```
X-XSS-Protection: 1; mode=block
```

**KEIN CSP-Header** — die Python-Implementierung setzt keinen CSP. Wir bleiben 1:1 kompatibel.

**Akzeptanzkriterien**:
- [ ] Alle 4 Header werden bei jeder Response gesetzt
- [ ] Bestehende Header werden nicht ueberschrieben (z.B. Content-Type)
- [ ] Header werden auch bei Error-Responses gesetzt (da Middleware)

### 2.4 `internal/web/middleware/ratelimit.go` — IP-basiertes Rate-Limiting

```go
package middleware

// RateLimit erstellt eine IP-basierte Rate-Limiting-Middleware
func RateLimit(requestsPerWindow int, window time.Duration) func(http.Handler) http.Handler
```

**Implementierung**: Token-Bucket oder Sliding-Window pro Client-IP.

**Design-Entscheidungen**:
- IP-Extraktion: Primaer `r.RemoteAddr`, mit optionalem `X-Forwarded-For` / `X-Real-Ip` Trust (konfigurierbar)
- In-Memory-Map mit Cleanup-Goroutine (alle 2x Window-Dauer alte Eintraege entfernen)
- Default: 100 Requests pro Minute pro IP (Python hat kein explizites Rate-Limit, daher grosszuegig)
- Response bei Limit: 429 Too Many Requests mit `Retry-After` Header
- Static-Files (`/static/*`) vom Rate-Limit ausnehmen

**WICHTIG**: Die Python-Implementierung hat **KEIN** explizites Rate-Limiting. Dies ist eine Haertung gegenueber dem Original. Der Rate-Limiter sollte deshalb grosszuegig sein und leicht deaktivierbar.

```go
type ipEntry struct {
    tokens    int
    lastReset time.Time
}

type rateLimiter struct {
    mu       sync.Mutex
    entries  map[string]*ipEntry
    limit    int
    window   time.Duration
}
```

**Akzeptanzkriterien**:
- [ ] IP-basiert: verschiedene IPs haben separate Limits
- [ ] 429 Response mit `Retry-After` Header
- [ ] `/static/*` und `/api/health` vom Limit ausgenommen
- [ ] Cleanup-Goroutine verhindert Memory-Leak bei vielen unterschiedlichen IPs
- [ ] Thread-safe (sync.Mutex)
- [ ] Kein Panic bei Concurrent Access

### 2.5 `internal/web/middleware/logging.go` — Access-Logging

```go
package middleware

// AccessLogger erstellt eine Access-Logging-Middleware mit slog
func AccessLogger(logger *slog.Logger) func(http.Handler) http.Handler
```

**Log-Format** (slog strukturiert):
```
level=INFO msg="HTTP request" method=GET path=/api/version status=200 duration=1.2ms ip=127.0.0.1 request_id=abc123
```

**Details**:
- Nutzt `slog.With()` fuer strukturierte Felder
- Request-ID aus `middleware.GetReqID(r.Context())` (chi built-in)
- Dauer messen via `time.Since(start)`
- IP-Adresse aus `r.RemoteAddr`
- Status-Code ueber `ResponseWriter`-Wrapper (chi `middleware.WrapResponseWriter`)
- Log-Level: 2xx/3xx → INFO, 4xx → WARN, 5xx → ERROR
- `/static/*` Requests nicht loggen (oder auf DEBUG-Level) — zu viel Noise
- **NIEMALS** API-Key, Auth-Token oder MQTT-Key loggen

**Akzeptanzkriterien**:
- [ ] Jeder Request wird geloggt (ausser Static auf DEBUG-Level)
- [ ] Strukturierte Felder: method, path, status, duration, ip, request_id
- [ ] Log-Level abhaengig vom Status-Code
- [ ] Sensitive Daten werden NICHT geloggt
- [ ] Response-Body wird nicht gebuffert (Streaming-kompatibel)

### 2.6 `internal/web/middleware/devicecheck.go` — Drucker-Pruefungen

Zwei separate Middleware-Funktionen, die den Python `before_request`-Handlern entsprechen:

```go
package middleware

// DeviceState liefert den Server-Zustand fuer Device-Checks
type DeviceState interface {
    IsLoggedIn() bool
    IsUnsupportedDevice() bool
}

// RequirePrinter gibt 503 auf Drucker-Kontroll-Endpunkten wenn kein Drucker konfiguriert
func RequirePrinter(state DeviceState) func(http.Handler) http.Handler

// BlockUnsupportedDevice gibt 503 wenn das aktive Geraet nicht unterstuetzt wird
func BlockUnsupportedDevice(state DeviceState) func(http.Handler) http.Handler
```

**Printer-Control-Prefixes** (identisch zu Python):
```go
var printerControlPrefixes = []string{
    "/api/printer/",
    "/api/files/",
    "/api/filaments",
}
```

**Akzeptanzkriterien**:
- [ ] `/api/printer/*`, `/api/files/*`, `/api/filaments*` → 503 wenn kein Login
- [ ] `/api/printer/*`, `/api/files/*`, `/api/filaments*` → 503 wenn unsupported Device
- [ ] `/static/*` immer durchgelassen
- [ ] Andere Pfade immer durchgelassen
- [ ] JSON-Response: `{"error": "..."}`

### 2.7 `internal/web/middleware/bodysize.go` — Body-Size-Limit

```go
package middleware

// BodySizeLimit begrenzt die Request-Body-Groesse
func BodySizeLimit(maxBytes int64) func(http.Handler) http.Handler
```

**Implementierung**: Wrapped `r.Body` mit `http.MaxBytesReader`.

Python-Default: `UPLOAD_MAX_MB=2048` → 2 GB.

**Akzeptanzkriterien**:
- [ ] Requests mit Body > maxBytes erhalten 413 Request Entity Too Large
- [ ] Env-Variable `UPLOAD_MAX_MB` wird respektiert
- [ ] GET/HEAD/OPTIONS Requests werden nicht limitiert (kein Body)

### 2.8 `internal/web/middleware/session.go` — Session-Cookie-Handling

```go
package middleware

// SessionManager verwaltet signierte Session-Cookies
type SessionManager struct {
    secretKey []byte
    cookieName string
}

// NewSessionManager erstellt einen neuen SessionManager
func NewSessionManager(secretKey []byte) *SessionManager

// SetAuthenticated setzt das authenticated-Flag im Session-Cookie
func (sm *SessionManager) SetAuthenticated(w http.ResponseWriter, r *http.Request, value bool)

// IsAuthenticated prueft ob das authenticated-Flag gesetzt ist
func (sm *SessionManager) IsAuthenticated(r *http.Request) bool
```

**Session-Cookie-Eigenschaften** (wie Python):
- `SameSite=Strict`
- `HttpOnly=True`
- `Secure=False` (lokales Netzwerk, kein TLS)
- HMAC-SHA256-signiert mit dem Secret-Key
- Cookie-Name: `ankerctl_session`

**Secret-Key-Persistenz**:
- `FLASK_SECRET_KEY` ENV-Variable hat Vorrang
- Sonst: `~/.config/ankerctl/flask_secret.key` lesen/erstellen
- Datei-Permissions: 0600

**Akzeptanzkriterien**:
- [ ] Session-Cookie ist HMAC-signiert (Manipulationsschutz)
- [ ] `SameSite=Strict` und `HttpOnly=True`
- [ ] Secret-Key wird persistent gespeichert
- [ ] Ungueltige/manipulierte Cookies werden als nicht-authentifiziert behandelt

### 2.9 `internal/web/routes.go` — Stub-Router

Registriert alle Routen als Stubs, die 501 Not Implemented zurueckgeben. Dies dient als Pruefstand fuer die Middleware-Kette.

```go
package web

func (s *Server) registerRoutes() {
    r := s.router

    // --- Page Routes ---
    r.Get("/", s.stubHandler("index"))
    r.Get("/video", s.stubHandler("video"))

    // --- API Routes ---
    r.Get("/api/health", s.handleHealth)  // Einziger implementierter Handler
    r.Get("/api/version", s.stubHandler("version"))

    // Config
    r.Post("/api/ankerctl/config/upload", s.stubHandler("config-upload"))
    r.Post("/api/ankerctl/config/login", s.stubHandler("config-login"))
    r.Get("/api/ankerctl/server/reload", s.stubHandler("server-reload"))

    // Printer
    r.Get("/api/printers", s.stubHandler("printers-list"))
    r.Post("/api/printers/active", s.stubHandler("printers-switch"))
    r.Post("/api/printer/gcode", s.stubHandler("printer-gcode"))
    r.Post("/api/printer/control", s.stubHandler("printer-control"))
    r.Post("/api/printer/autolevel", s.stubHandler("printer-autolevel"))
    r.Get("/api/printer/bed-leveling", s.stubHandler("bed-leveling"))
    r.Get("/api/printer/bed-leveling/last", s.stubHandler("bed-leveling-last"))

    // Files (OctoPrint compat)
    r.Post("/api/files/local", s.stubHandler("files-local"))
    r.Post("/api/ankerctl/config/upload-rate", s.stubHandler("upload-rate"))

    // Snapshot
    r.Get("/api/snapshot", s.stubHandler("snapshot"))

    // Notifications
    r.Get("/api/notifications/settings", s.stubHandler("notifications-settings-get"))
    r.Post("/api/notifications/settings", s.stubHandler("notifications-settings-post"))
    r.Post("/api/notifications/test", s.stubHandler("notifications-test"))

    // Settings
    r.Get("/api/settings/timelapse", s.stubHandler("settings-timelapse-get"))
    r.Post("/api/settings/timelapse", s.stubHandler("settings-timelapse-post"))
    r.Get("/api/settings/mqtt", s.stubHandler("settings-mqtt-get"))
    r.Post("/api/settings/mqtt", s.stubHandler("settings-mqtt-post"))

    // History
    r.Get("/api/history", s.stubHandler("history-list"))
    r.Delete("/api/history", s.stubHandler("history-clear"))

    // Filaments
    r.Get("/api/filaments", s.stubHandler("filaments-list"))
    r.Post("/api/filaments", s.stubHandler("filaments-create"))
    r.Put("/api/filaments/{id}", s.stubHandler("filaments-update"))
    r.Delete("/api/filaments/{id}", s.stubHandler("filaments-delete"))
    r.Post("/api/filaments/{id}/apply", s.stubHandler("filaments-apply"))
    r.Post("/api/filaments/{id}/duplicate", s.stubHandler("filaments-duplicate"))

    // Timelapses
    r.Get("/api/timelapses", s.stubHandler("timelapses-list"))
    r.Get("/api/timelapse/{filename}", s.stubHandler("timelapse-download"))
    r.Delete("/api/timelapse/{filename}", s.stubHandler("timelapse-delete"))

    // Debug (nur wenn ANKERCTL_DEV_MODE=true)
    if s.devMode {
        r.Get("/api/debug/state", s.stubHandler("debug-state"))
        r.Post("/api/debug/config", s.stubHandler("debug-config"))
        r.Post("/api/debug/simulate", s.stubHandler("debug-simulate"))
        r.Get("/api/debug/logs", s.stubHandler("debug-logs-list"))
        r.Get("/api/debug/logs/{filename}", s.stubHandler("debug-logs-content"))
        r.Get("/api/debug/services", s.stubHandler("debug-services"))
        r.Post("/api/debug/services/{name}/restart", s.stubHandler("debug-service-restart"))
        r.Post("/api/debug/services/{name}/test", s.stubHandler("debug-service-test"))
        r.Get("/api/debug/bed-leveling", s.stubHandler("debug-bed-leveling"))
    }

    // WebSocket-Endpunkte (Stubs, Phase 11)
    r.Get("/ws/mqtt", s.stubHandler("ws-mqtt"))
    r.Get("/ws/video", s.stubHandler("ws-video"))
    r.Get("/ws/pppp-state", s.stubHandler("ws-pppp-state"))
    r.Get("/ws/upload", s.stubHandler("ws-upload"))
    r.Get("/ws/ctrl", s.stubHandler("ws-ctrl"))

    // Static Files
    // Phase 14: r.Handle("/static/*", http.StripPrefix("/static/", staticFileServer))
}
```

**Health-Endpunkt** (einziger implementierter Handler):
```go
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    w.Write([]byte(`{"status":"ok"}`))
}
```

**Akzeptanzkriterien**:
- [ ] Alle 40+ Routen registriert (Stub → 501)
- [ ] `/api/health` gibt 200 `{"status":"ok"}` zurueck
- [ ] Debug-Routen nur registriert wenn `ANKERCTL_DEV_MODE=true`
- [ ] HTTP-Methoden korrekt (GET vs POST vs DELETE vs PUT)
- [ ] Pfad-Parameter korrekt: `{id}`, `{filename}`, `{name}`

---

## 3. Dateiliste und Implementierungsreihenfolge

| # | Datei | Abhaengigkeit | Komplexitaet |
|---|---|---|---|
| 1 | `internal/web/middleware/security.go` | keine | Niedrig |
| 2 | `internal/web/middleware/logging.go` | keine | Niedrig |
| 3 | `internal/web/middleware/bodysize.go` | keine | Niedrig |
| 4 | `internal/web/middleware/ratelimit.go` | keine | Mittel |
| 5 | `internal/web/middleware/session.go` | keine | Mittel |
| 6 | `internal/web/middleware/devicecheck.go` | Server-Interface | Mittel |
| 7 | `internal/web/middleware/auth.go` | session.go, Server-Interface | Hoch |
| 8 | `internal/web/server.go` | alle Middleware | Hoch |
| 9 | `internal/web/routes.go` | server.go | Niedrig |

**Empfohlene Reihenfolge**: 1 → 2 → 3 → 4 → 5 → 6 → 7 → 8 → 9

---

## 4. Risiken und Fallstricke

### 4.1 Session-Handling (HOCH)

**Problem**: Flask hat eingebautes Session-Management mit signierten Cookies. Go hat das nicht.

**Risiko**: Falsche Session-Implementierung kann Auth-Bypass ermoeglichen.

**Mitigation**: HMAC-SHA256-signierte Cookies verwenden. Payload ist minimal: nur `{"authenticated": true}`. Base64-encoded, mit HMAC-Signatur prefixed. Keine sensiblen Daten im Cookie.

**Alternative**: `gorilla/sessions` Bibliothek — gut getestet, weit verbreitet. Overhead: eine zusaetzliche Dependency.

### 4.2 Auth-Reihenfolge (HOCH)

**Problem**: Die Reihenfolge der Auth-Pruefungen ist sicherheitskritisch. Falsche Reihenfolge kann dazu fuehren, dass geschuetzte Endpunkte offen sind.

**Risiko**: Python prueft `?apikey=` VOR dem GET-Shortcut. Das bedeutet: wenn jemand `?apikey=wrong` an einen GET-Endpunkt sendet, wird der GET-Shortcut trotzdem greifen (kein 401). Nur bei POST/DELETE und Protected-GETs ist der Key zwingend.

**Mitigation**: Unit-Tests fuer JEDEN Auth-Pfad. Testmatrix:
- GET ohne Key auf offenen Pfad → 200
- GET ohne Key auf Protected Pfad → 401
- GET mit falschem Key auf Protected Pfad → 401
- GET mit richtigem Key → 200 (via Header oder Cookie)
- POST ohne Key → 401
- POST mit richtigem Key → 200
- POST auf Setup-Path ohne Login → 200
- POST auf Setup-Path mit Login → 401 (Key erforderlich)

### 4.3 Rate-Limit-Bypass bei Proxies (MITTEL)

**Problem**: Wenn der Server hinter einem Reverse-Proxy laeuft, haben alle Requests die gleiche RemoteAddr.

**Risiko**: Rate-Limit greift global statt pro Client.

**Mitigation**:
- `X-Forwarded-For` und `X-Real-Ip` Header optionally trusten
- Da der typische Einsatz `network_mode: host` in Docker ist, ist dies ein geringes Risiko
- Konfigurierbar ueber `ANKERCTL_TRUST_PROXY` ENV-Variable (Default: false)

### 4.4 Body-Size-Limit vs WebSocket (MITTEL)

**Problem**: WebSocket-Verbindungen sollten nicht durch Body-Size-Limit blockiert werden.

**Mitigation**: `http.MaxBytesReader` gilt nur fuer den initialen HTTP-Body, nicht fuer WebSocket-Frames. WebSocket-Upgrade-Requests haben typischerweise keinen Body. Trotzdem: `/ws/*` Pfade vom Body-Size-Limit ausnehmen.

### 4.5 `?apikey=` Redirect-Loop (NIEDRIG)

**Problem**: Python macht einen Redirect um den `?apikey=` Parameter aus der URL zu entfernen. Wenn der Redirect-Target wieder `?apikey=` enthaelt (z.B. durch Browser-Cache), entsteht eine Loop.

**Mitigation**: Der Redirect entfernt NUR den `apikey`-Parameter, behaelt alle anderen Query-Parameter bei. HTTP 302 (nicht 301) verwenden, damit Browser nicht cachen.

### 4.6 Middleware-Reihenfolge _require_printer vs _check_api_key (MITTEL)

**Problem**: In Python laufen die before_request-Handler in Definitions-Reihenfolge. `_require_printer_for_control` laeuft VOR `_check_api_key`. Das bedeutet: ein 503 wegen "kein Drucker" wird auch ohne Auth zurueckgegeben.

**Konsequenz fuer Go**: Device-Check-Middleware MUSS vor Auth-Middleware in der Kette stehen.

---

## 5. Nicht in Python vorhandene Haertungen

Diese Sicherheitsmassnahmen gehen ueber die 1:1-Portierung hinaus:

| Feature | Python | Go (Phase 4) | Begruendung |
|---|---|---|---|
| Rate-Limiting | Nicht vorhanden | 100 req/min/IP | DoS-Schutz |
| Request-ID | Nicht vorhanden | chi Middleware | Debugging |
| Panic-Recovery | Flask built-in | chi Middleware | Explizit |
| Body-Size-Limit | `MAX_CONTENT_LENGTH` | `http.MaxBytesReader` | Equivalent |

Diese sind in MIGRATION_PLAN.md Phase 4 bereits vorgesehen und stellen **bewusste Haertungen** dar.

---

## 6. Test-Strategie

### 6.1 Unit-Tests pro Middleware

Jede Middleware erhaelt eine eigene Test-Datei:

- `middleware/auth_test.go` — Auth-Pruefungsmatrix (mindestens 15 Tests)
- `middleware/security_test.go` — Header-Pruefung
- `middleware/ratelimit_test.go` — Limit-Pruefung, Cleanup, Concurrent Access
- `middleware/logging_test.go` — Log-Output-Pruefung
- `middleware/devicecheck_test.go` — Printer-Required + Unsupported-Device
- `middleware/bodysize_test.go` — Size-Limit-Pruefung
- `middleware/session_test.go` — Cookie-Signatur, Manipulation, Expiry

### 6.2 Integration-Tests

- `server_test.go` — Voller Middleware-Stack, Health-Endpunkt
- Prueft dass die Middleware-Kette korrekt zusammenspielt
- Testet Edge-Cases: Auth + Rate-Limit gleichzeitig etc.

### 6.3 Besonders kritische Tests fuer Auth

```
TestAuth_NoKeyConfigured_AllRequestsPass
TestAuth_PostWithoutKey_Returns401
TestAuth_PostWithHeaderKey_Returns200
TestAuth_PostWithSessionCookie_Returns200
TestAuth_GetOnOpenPath_Returns200
TestAuth_GetOnProtectedPath_Returns401
TestAuth_GetOnDebugPath_Returns401
TestAuth_GetOnDebugSubpath_Returns401
TestAuth_SetupPathWithoutLogin_Returns200
TestAuth_SetupPathWithLogin_Returns401
TestAuth_ApikeyQueryParam_SetsCookieAndRedirects
TestAuth_InvalidApikeyQueryParam_NoRedirect
TestAuth_StaticPathAlwaysAllowed
```

---

## 7. Go-Dependency-Ergaenzungen

Fuer Phase 4 werden **keine neuen** externen Dependencies benoetigt, die nicht bereits in `go.mod` stehen:

- `github.com/go-chi/chi/v5` — bereits in go.mod (Phase 1)
- `crypto/hmac`, `crypto/sha256` — stdlib
- `log/slog` — stdlib
- `net/http` — stdlib
- `sync` — stdlib

Optional (Entscheidung bei Implementierung):
- `github.com/gorilla/sessions` — fuer robusteres Session-Handling
- Empfehlung: NICHT verwenden, eigene HMAC-Cookie-Implementierung ist einfacher und hat keine Dependency

---

## 8. Vollstaendige Routen-Referenz (aus Python extrahiert)

Komplett-Liste aller HTTP-Endpunkte mit Methode und Auth-Anforderung:

| Methode | Pfad | Auth | Anmerkung |
|---|---|---|---|
| GET | `/` | Nein | Homepage |
| GET | `/video` | Spezial* | Video-Stream |
| GET | `/api/health` | Nein | Liveness-Probe |
| GET | `/api/version` | Nein | OctoPrint-Compat |
| POST | `/api/ankerctl/config/upload` | Setup** | Config-Import |
| POST | `/api/ankerctl/config/login` | Setup** | Login |
| GET | `/api/ankerctl/server/reload` | Ja (Protected GET) | Server neuladen |
| POST | `/api/ankerctl/config/upload-rate` | Ja | Upload-Rate aendern |
| GET | `/api/printers` | Ja (Protected GET) | Drucker-Liste |
| POST | `/api/printers/active` | Ja | Drucker wechseln |
| POST | `/api/printer/gcode` | Ja | GCode senden |
| POST | `/api/printer/control` | Ja | Print-Kontrolle |
| POST | `/api/printer/autolevel` | Ja | Auto-Leveling |
| GET | `/api/printer/bed-leveling` | Nein | Bed-Leveling lesen |
| GET | `/api/printer/bed-leveling/last` | Nein | Letztes Bed-Leveling |
| POST | `/api/files/local` | Ja | Datei-Upload (OctoPrint) |
| GET | `/api/snapshot` | Nein | JPEG-Snapshot |
| GET | `/api/notifications/settings` | Ja (Protected GET) | Apprise-Settings |
| POST | `/api/notifications/settings` | Ja | Apprise-Settings aendern |
| POST | `/api/notifications/test` | Ja | Test-Notification |
| GET | `/api/settings/timelapse` | Nein | Timelapse-Settings |
| POST | `/api/settings/timelapse` | Ja | Timelapse-Settings aendern |
| GET | `/api/settings/mqtt` | Ja (Protected GET) | HA-MQTT-Settings |
| POST | `/api/settings/mqtt` | Ja | HA-MQTT-Settings aendern |
| GET | `/api/history` | Ja (Protected GET) | Print-History |
| DELETE | `/api/history` | Ja | History loeschen |
| GET | `/api/filaments` | Nein | Filament-Liste |
| POST | `/api/filaments` | Ja | Filament erstellen |
| PUT | `/api/filaments/{id}` | Ja | Filament aendern |
| DELETE | `/api/filaments/{id}` | Ja | Filament loeschen |
| POST | `/api/filaments/{id}/apply` | Ja | Filament anwenden |
| POST | `/api/filaments/{id}/duplicate` | Ja | Filament duplizieren |
| GET | `/api/timelapses` | Nein | Timelapse-Videos |
| GET | `/api/timelapse/{filename}` | Nein | Video herunterladen |
| DELETE | `/api/timelapse/{filename}` | Ja | Video loeschen |
| GET | `/api/debug/state` | Ja (Debug) | MQTT-State |
| POST | `/api/debug/config` | Ja (Debug) | Debug-Config |
| POST | `/api/debug/simulate` | Ja (Debug) | Event simulieren |
| GET | `/api/debug/logs` | Ja (Debug) | Log-Dateien |
| GET | `/api/debug/logs/{filename}` | Ja (Debug) | Log-Inhalt |
| GET | `/api/debug/services` | Ja (Debug) | Service-Status |
| POST | `/api/debug/services/{name}/restart` | Ja (Debug) | Service neustarten |
| POST | `/api/debug/services/{name}/test` | Ja (Debug) | Service testen |
| GET | `/api/debug/bed-leveling` | Ja (Debug) | Debug Bed-Leveling |

\* `/video` hat eigene Auth-Logik: Session-Cookie ODER `X-Api-Key` Header ODER `?apikey=` Parameter. Kein Redirect bei apikey-Parameter.

\** Setup-Paths: Auth nur wenn Drucker bereits konfiguriert (`login=true`).

---

## 9. Vergleich Python-Reihenfolge vs Go-Middleware-Reihenfolge

```
Python (before_request order):
  1. _require_printer_for_control()
  2. _block_unsupported_device()
  3. _check_api_key()
  + after_request: add_security_headers()

Go (chi Middleware order, aussen nach innen):
  1. Recovery (Panic-Handler) — NEU
  2. RequestID — NEU
  3. AccessLogger — NEU
  4. SecurityHeaders — equivalent zu after_request
  5. RateLimit — NEU
  6. BodySizeLimit — equivalent zu MAX_CONTENT_LENGTH
  7. RequirePrinter — equivalent zu _require_printer_for_control
  8. BlockUnsupportedDevice — equivalent zu _block_unsupported_device
  9. Auth — equivalent zu _check_api_key
```

Die Python-Reihenfolge bleibt erhalten: Device-Checks vor Auth.
Zusaetzliche Middleware (Recovery, RequestID, Logging, RateLimit) kommen davor.

---

## 10. Offene Entscheidungen

| # | Frage | Optionen | Empfehlung |
|---|---|---|---|
| 1 | Session-Bibliothek | Eigene HMAC-Cookies vs gorilla/sessions | Eigene (weniger Deps) |
| 2 | Rate-Limit-Algorithmus | Token-Bucket vs Fixed-Window | Fixed-Window (einfacher) |
| 3 | Trust-Proxy-Header | X-Forwarded-For trusten? | Nein (Default), ENV konfigurierbar |
| 4 | X-XSS-Protection | Hinzufuegen (nicht in Python) | Ja (kostet nichts) |
| 5 | Rate-Limit-Default | Wieviel req/min? | 100/min (grosszuegig, Python hat keins) |
