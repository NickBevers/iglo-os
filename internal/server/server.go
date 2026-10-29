package server

import (
	"context"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/coder/websocket"

	"iglo/internal/agent"
	"iglo/internal/apps"
	"iglo/internal/auth"
	"iglo/internal/backups"
	"iglo/internal/events"
	"iglo/internal/health"
	"iglo/internal/jellyfin"
	"iglo/internal/proxmox"
	"iglo/internal/selfupdate"
	"iglo/internal/settingsfile"
	"iglo/internal/tailscale"
	"iglo/internal/updates"
	"iglo/internal/workloads"
)

const sessionCookie = "iglo_session"

// csrfHeader must be sent on every state-changing request. Browsers can't add
// custom headers to cross-site requests without CORS, which we don't allow.
const csrfHeader = "X-Iglo-Request"

type Proxmox interface {
	Resources(ctx context.Context) ([]proxmox.Resource, error)
	NodeStatus(ctx context.Context, node string) (proxmox.NodeStatus, error)
	RunGuestAction(ctx context.Context, node string, guestType proxmox.GuestType, vmid int, action proxmox.GuestAction) (string, error)
	NodeUsage(ctx context.Context, node string, timeframe proxmox.Timeframe) ([]proxmox.UsagePoint, error)
	GuestUsage(ctx context.Context, node string, guestType proxmox.GuestType, vmid int, timeframe proxmox.Timeframe) ([]proxmox.UsagePoint, error)
	Tasks(ctx context.Context, node string, limit int) ([]proxmox.Task, error)
	TaskLog(ctx context.Context, node, upid string, limit int) ([]string, error)
}

// Console opens a container console through the host agent.
type Console interface {
	OpenConsole(ctx context.Context, vmid int) (*websocket.Conn, error)
}

// Logs reads journals and Docker logs through the host agent.
type Logs interface {
	Journal(ctx context.Context, query agent.JournalQuery) ([]agent.LogEntry, error)
	DockerLogs(ctx context.Context, vmid, lines int) (agent.DockerLogs, error)
}

type Agent interface {
	Health(ctx context.Context) (agent.Health, error)
}

type Updates interface {
	Status(ctx context.Context) (updates.View, error)
	Run(id string) (updates.Run, error)
	StartCheck(ctx context.Context) error
	StartHostUpdate(ctx context.Context) error
	StartGuestUpdate(ctx context.Context, vmid int) error
	SaveSettings(input updates.SettingsInput) error
	SendTestNotification(ctx context.Context) error
}

type Jellyfin interface {
	Status(ctx context.Context) jellyfin.View
	SaveSettings(ctx context.Context, input jellyfin.Settings) error
	SetTheme(ctx context.Context, enabled bool) error
	Image(ctx context.Context, itemID, imageType string, maxWidth int) (*http.Response, error)
}

type Health interface {
	Status() health.View
	CheckSystem(ctx context.Context)
	CheckServices(ctx context.Context)
	AddCheck(input health.CheckInput) (health.Check, error)
	UpdateCheck(id string, input health.CheckInput) error
	DeleteCheck(id string) error
}

type SelfUpdate interface {
	Status(ctx context.Context) selfupdate.View
	Check(ctx context.Context) error
	Install(ctx context.Context) error
	SaveSettings(input selfupdate.SettingsInput) error
}

type Tailscale interface {
	Status(ctx context.Context) tailscale.View
	Connect(background context.Context, authKey string) error
	Logout(ctx context.Context) error
	SetServe(ctx context.Context, enabled bool) error
	SaveSettings(ctx context.Context, settings tailscale.Settings) error
	UseTag(background context.Context) error
	SetService(ctx context.Context, name string, published bool) error
}

type Backups interface {
	Status(ctx context.Context) (backups.View, error)
	SaveJob(ctx context.Context, job agent.BackupJob) error
	BackUp(ctx context.Context, vmid int) error
	Restore(ctx context.Context, vmid int, volid string) error
	// Check restores a backup to a copy, starts it and removes it again.
	Check(ctx context.Context, volid string) error
	Delete(ctx context.Context, volid string) error
}

type Apps interface {
	Status(ctx context.Context) (apps.View, error)
	Install(ctx context.Context, background context.Context, id string, request agent.InstallRequest) error
	Retry(ctx context.Context, background context.Context, id string) error
	Forget(ctx context.Context, id string) error
	Update(ctx context.Context, background context.Context, id string) error
	Remove(ctx context.Context, background context.Context, id string) error
	VPN(ctx context.Context, id string) (agent.VPNView, error)
	ChangeVPN(ctx context.Context, background context.Context, id string, settings agent.VPNSettings) error
	Players(ctx context.Context, id string) (agent.MinecraftPlayers, error)
	ChangePlayers(ctx context.Context, background context.Context, id string, players agent.MinecraftPlayers) error
}

type SettingsFile interface {
	Export(ctx context.Context) ([]byte, error)
	Import(ctx context.Context, data []byte, apply bool) (settingsfile.Result, error)
}

// Covers finds box art for games that have no cover of their own.
type Covers interface {
	Cover(ctx context.Context, system, name string) ([]byte, error)
}

type Workloads interface {
	Status() workloads.View
	SaveSettings(workloads.Settings) error
	Protect(minutes int) error
}

type Options struct {
	Auth       *auth.Service
	Proxmox    Proxmox
	Agent      Agent
	Updates    Updates
	Jellyfin   Jellyfin
	Health     Health
	SelfUpdate SelfUpdate
	Tailscale  Tailscale
	Backups    Backups
	Logs       Logs
	Console    Console
	Apps       Apps
	Workloads  Workloads
	Covers     Covers
	// Events tells the open pages which topic changed.
	Events *events.Hub
	// Setup installs the whole stack in one guided run.
	Setup Setup
	// SettingsFile exports and imports iglo.yaml.
	SettingsFile SettingsFile
	// DataDir is the folder that a data backup copies.
	DataDir string
	// Restart stops the manager, so systemd starts it again with restored data.
	Restart func()
	// Notify sends a message with the Telegram settings, for example when a console opens.
	Notify func(ctx context.Context, text string)
	// Background is the context for work that outlives a request, like updates.
	Background context.Context
	Web        fs.FS
	// SecureCookies is false only in dev mode, where we serve plain HTTP.
	SecureCookies bool
	Logger        *slog.Logger
}

type server struct {
	Options
}

func New(options Options) http.Handler {
	s := &server{Options: options}
	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("POST /api/auth/logout", s.logout)
	mux.Handle("GET /api/auth/me", s.requireSession(http.HandlerFunc(s.me)))
	mux.Handle("GET /api/overview", s.requireSession(http.HandlerFunc(s.overview)))
	mux.Handle("GET /api/events", s.requireSession(http.HandlerFunc(s.serverEvents)))
	mux.Handle("GET /api/setup", s.requireSession(http.HandlerFunc(s.setupRoute)))
	mux.Handle("POST /api/setup", s.requireSession(http.HandlerFunc(s.setupRoute)))
	mux.Handle("DELETE /api/setup", s.requireSession(http.HandlerFunc(s.setupRoute)))
	mux.Handle("POST /api/setup/{action}", s.requireSession(http.HandlerFunc(s.setupRoute)))
	mux.Handle("GET /api/setup/{action}", s.requireSession(http.HandlerFunc(s.setupRoute)))
	mux.Handle("GET /api/storage/bindings", s.requireSession(http.HandlerFunc(s.storageBindings)))
	mux.Handle("POST /api/storage/bindings", s.requireSession(http.HandlerFunc(s.storageBindings)))
	mux.Handle("DELETE /api/storage/bindings/{id}", s.requireSession(http.HandlerFunc(s.storageBindings)))
	mux.Handle("PUT /api/storage/bindings/{id}/proxmox-backups", s.requireSession(http.HandlerFunc(s.bindingBackups)))
	mux.Handle("GET /api/workloads", s.requireSession(http.HandlerFunc(s.workloadsStatus)))
	mux.Handle("PUT /api/workloads/settings", s.requireSession(http.HandlerFunc(s.workloadsSettings)))
	mux.Handle("PUT /api/workloads/protection", s.requireSession(http.HandlerFunc(s.workloadsProtection)))
	mux.Handle("POST /api/guests/{node}/{type}/{vmid}/{action}", s.requireSession(http.HandlerFunc(s.guestAction)))
	mux.Handle("GET /api/usage/{node}", s.requireSession(http.HandlerFunc(s.nodeUsage)))
	mux.Handle("GET /api/usage/{node}/{type}/{vmid}", s.requireSession(http.HandlerFunc(s.guestUsage)))
	mux.Handle("GET /api/updates", s.requireSession(http.HandlerFunc(s.updatesStatus)))
	mux.Handle("GET /api/updates/runs/{id}", s.requireSession(http.HandlerFunc(s.updatesRun)))
	mux.Handle("POST /api/updates/check", s.requireSession(http.HandlerFunc(s.updatesCheck)))
	mux.Handle("POST /api/updates/host", s.requireSession(http.HandlerFunc(s.updatesHost)))
	mux.Handle("POST /api/updates/guests/{vmid}", s.requireSession(http.HandlerFunc(s.updatesGuest)))
	mux.Handle("PUT /api/updates/settings", s.requireSession(http.HandlerFunc(s.updatesSettings)))
	mux.Handle("POST /api/updates/test-notification", s.requireSession(http.HandlerFunc(s.updatesTestNotification)))
	mux.Handle("GET /api/jellyfin", s.requireSession(http.HandlerFunc(s.jellyfinStatus)))
	mux.Handle("PUT /api/jellyfin/settings", s.requireSession(http.HandlerFunc(s.jellyfinSettings)))
	mux.Handle("PUT /api/jellyfin/theme", s.requireSession(http.HandlerFunc(s.jellyfinTheme)))
	mux.Handle("GET /api/jellyfin/items/{id}/image", s.requireSession(http.HandlerFunc(s.jellyfinImage)))
	mux.Handle("GET /api/health", s.requireSession(http.HandlerFunc(s.healthStatus)))
	mux.Handle("POST /api/health/refresh", s.requireSession(http.HandlerFunc(s.healthRefresh)))
	mux.Handle("POST /api/health/checks", s.requireSession(http.HandlerFunc(s.healthAddCheck)))
	mux.Handle("PUT /api/health/checks/{id}", s.requireSession(http.HandlerFunc(s.healthUpdateCheck)))
	mux.Handle("DELETE /api/health/checks/{id}", s.requireSession(http.HandlerFunc(s.healthDeleteCheck)))
	mux.Handle("GET /api/self-update", s.requireSession(http.HandlerFunc(s.selfUpdateStatus)))
	mux.Handle("POST /api/self-update/check", s.requireSession(http.HandlerFunc(s.selfUpdateCheck)))
	mux.Handle("POST /api/self-update/install", s.requireSession(http.HandlerFunc(s.selfUpdateInstall)))
	mux.Handle("PUT /api/self-update/settings", s.requireSession(http.HandlerFunc(s.selfUpdateSettings)))
	mux.Handle("GET /api/tailscale", s.requireSession(http.HandlerFunc(s.tailscaleStatus)))
	mux.Handle("POST /api/tailscale/connect", s.requireSession(http.HandlerFunc(s.tailscaleConnect)))
	mux.Handle("POST /api/tailscale/logout", s.requireSession(http.HandlerFunc(s.tailscaleLogout)))
	mux.Handle("PUT /api/tailscale/serve", s.requireSession(http.HandlerFunc(s.tailscaleServe)))
	mux.Handle("PUT /api/tailscale/settings", s.requireSession(http.HandlerFunc(s.tailscaleSettings)))
	mux.Handle("POST /api/tailscale/tag", s.requireSession(http.HandlerFunc(s.tailscaleUseTag)))
	mux.Handle("PUT /api/tailscale/services/{name}", s.requireSession(http.HandlerFunc(s.tailscaleService)))
	mux.Handle("GET /api/backups", s.requireSession(http.HandlerFunc(s.backupsStatus)))
	mux.Handle("PUT /api/backups/job", s.requireSession(http.HandlerFunc(s.backupsSaveJob)))
	mux.Handle("POST /api/backups/guests/{vmid}", s.requireSession(http.HandlerFunc(s.backupsBackUp)))
	mux.Handle("POST /api/backups/restore", s.requireSession(http.HandlerFunc(s.backupsRestore)))
	mux.Handle("POST /api/backups/delete", s.requireSession(http.HandlerFunc(s.backupsDelete)))
	mux.Handle("POST /api/backups/check", s.requireSession(http.HandlerFunc(s.backupsCheck)))
	mux.Handle("GET /api/logs/journal", s.requireSession(http.HandlerFunc(s.logsJournal)))
	mux.Handle("GET /api/logs/docker", s.requireSession(http.HandlerFunc(s.logsDocker)))
	mux.Handle("GET /api/logs/tasks", s.requireSession(http.HandlerFunc(s.logsTasks)))
	mux.Handle("GET /api/logs/tasks/{node}/log", s.requireSession(http.HandlerFunc(s.logsTaskLog)))
	mux.Handle("GET /api/guests/{vmid}/console", s.requireSession(http.HandlerFunc(s.console)))
	mux.Handle("GET /api/apps", s.requireSession(http.HandlerFunc(s.appsStatus)))
	mux.Handle("POST /api/photos/recovery/{action}", s.requireSession(http.HandlerFunc(s.photosRecovery)))
	for _, route := range []string{"GET /api/inbox", "POST /api/inbox", "PUT /api/inbox/settings", "POST /api/inbox/scan", "POST /api/inbox/retry/{id}", "POST /api/inbox/migrate"} {
		mux.Handle(route, s.requireSession(http.HandlerFunc(s.inboxWork)))
	}
	mux.Handle("GET /api/music/jobs", s.requireSession(http.HandlerFunc(s.musicJobs)))
	mux.Handle("POST /api/music/jobs", s.requireSession(http.HandlerFunc(s.musicJobs)))
	mux.Handle("PUT /api/music/pause", s.requireSession(http.HandlerFunc(s.musicJobs)))
	mux.Handle("GET /api/gaming", s.requireSession(http.HandlerFunc(s.gamingStatus)))
	mux.Handle("PUT /api/gaming/play", s.requireSession(http.HandlerFunc(s.gamingPlay)))
	mux.Handle("POST /api/gaming/sessions/{id}/stop", s.requireSession(http.HandlerFunc(s.gamingStopSession)))
	mux.Handle("GET /api/gaming/pairing", s.requireSession(http.HandlerFunc(s.gamingPairings)))
	mux.Handle("POST /api/gaming/pairing/{id}", s.requireSession(http.HandlerFunc(s.gamingPair)))
	mux.Handle("GET /api/gaming/games", s.requireSession(http.HandlerFunc(s.gamingGames)))
	mux.Handle("GET /api/gaming/covers/{system}/{name}", s.requireSession(http.HandlerFunc(s.gamingCover)))
	mux.Handle("POST /api/apps/{id}/install", s.requireSession(http.HandlerFunc(s.appsInstall)))
	mux.Handle("POST /api/apps/{id}/retry", s.requireSession(http.HandlerFunc(s.appsRetry)))
	mux.Handle("DELETE /api/apps/{id}/answers", s.requireSession(http.HandlerFunc(s.appsForget)))
	mux.Handle("POST /api/apps/{id}/update", s.requireSession(http.HandlerFunc(s.appsUpdate)))
	mux.Handle("POST /api/apps/{id}/remove", s.requireSession(http.HandlerFunc(s.appsRemove)))
	mux.Handle("GET /api/apps/{id}/vpn", s.requireSession(http.HandlerFunc(s.appsVPN)))
	mux.Handle("PUT /api/apps/{id}/vpn", s.requireSession(http.HandlerFunc(s.appsChangeVPN)))
	mux.Handle("GET /api/apps/{id}/players", s.requireSession(http.HandlerFunc(s.appsPlayers)))
	mux.Handle("PUT /api/apps/{id}/players", s.requireSession(http.HandlerFunc(s.appsChangePlayers)))
	mux.Handle("GET /api/settings/export", s.requireSession(http.HandlerFunc(s.settingsExport)))
	mux.Handle("POST /api/settings/import", s.requireSession(http.HandlerFunc(s.settingsImport)))
	mux.Handle("POST /api/data-backup/download", s.requireSession(http.HandlerFunc(s.dataBackupDownload)))
	mux.Handle("POST /api/data-backup/restore", s.requireSession(http.HandlerFunc(s.dataBackupRestore)))
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
	mux.Handle("/", spa(options.Web))

	return securityHeaders(csrf(compressJSON(mux)))
}

func (s *server) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil || !s.Auth.Validate(cookie.Value) {
			writeError(w, http.StatusUnauthorized, "not logged in")
			return
		}

		next.ServeHTTP(w, r)
	})
}

func csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		safe := r.Method == http.MethodGet || r.Method == http.MethodHead
		if !safe && strings.HasPrefix(r.URL.Path, "/api/") && r.Header.Get(csrfHeader) != "1" {
			writeError(w, http.StatusForbidden, "missing "+csrfHeader+" header")
			return
		}

		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		// style-src needs 'unsafe-inline' for Radix positioning and the theme switcher.
		header.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("Referrer-Policy", "no-referrer")
		header.Set("X-Frame-Options", "DENY")
		if r.TLS != nil {
			header.Set("Strict-Transport-Security", "max-age=31536000")
		}

		next.ServeHTTP(w, r)
	})
}

func spa(files fs.FS) http.Handler {
	static := &staticFiles{files: files}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")

		if path != "" {
			if info, err := fs.Stat(files, path); err == nil && !info.IsDir() {
				if strings.HasPrefix(path, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				static.serve(w, r, path)
				return
			}
		}

		w.Header().Set("Cache-Control", "no-cache")
		static.serve(w, r, "index.html")
	})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return host
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
