package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"iglo/internal/appproxy"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	"iglo/internal/agent"
	"iglo/internal/apps"
	"iglo/internal/auth"
	"iglo/internal/backups"
	"iglo/internal/config"
	"iglo/internal/covers"
	"iglo/internal/databackup"
	"iglo/internal/health"
	"iglo/internal/jellyfin"
	"iglo/internal/proxmox"
	"iglo/internal/selfupdate"
	"iglo/internal/server"
	"iglo/internal/settingsfile"
	"iglo/internal/tailscale"
	"iglo/internal/tlsca"
	"iglo/internal/updates"
	"iglo/internal/workloads"
	"iglo/web"
)

func serve() error {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Before anything reads the data, so no service writes over a restore.
	if restored, err := databackup.Apply(cfg.DataDir); err != nil {
		return fmt.Errorf("apply the restored data backup: %w", err)
	} else if restored {
		logger.Info("applied the restored data backup")
	}

	admin, err := auth.LoadAdmin(auth.AdminPath(cfg.DataDir))
	if errors.Is(err, os.ErrNotExist) {
		return errors.New("no admin account yet, run `iglo admin` first")
	}
	if err != nil {
		return err
	}

	pve, err := proxmox.New(cfg.Proxmox)
	if err != nil {
		return err
	}

	webFS, err := web.Dist()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, restart := context.WithCancelCause(ctx)
	defer restart(nil)
	restartCtx := ctx

	agentClient := agent.NewClient(cfg.AgentSocket)
	workloadService, err := workloads.New(workloads.Options{DataDir: cfg.DataDir, Proxmox: pve, WorkerAdmission: admissionSender(pve.Resources, agentClient.WorkerAdmission, time.Now)})
	if err != nil {
		return err
	}
	go workloadService.Run(ctx)
	updateService, err := updates.New(updates.Options{
		DataDir:  cfg.DataDir,
		Proxmox:  pve,
		Agent:    agentClient,
		SelfVMID: cfg.SelfVMID,
		Logger:   logger,
	})
	if err != nil {
		return err
	}
	go updateService.RunScheduler(ctx)

	jellyfinService, err := jellyfin.NewService(cfg.DataDir)
	if err != nil {
		return err
	}

	healthService, err := health.New(health.Options{
		DataDir: cfg.DataDir,
		Proxmox: pve,
		Agent:   agentClient,
		Notify:  updateService.Notify,
		Logger:  logger,
	})
	if err != nil {
		return err
	}
	go healthService.Run(ctx)

	selfUpdateService, err := selfupdate.New(selfupdate.Options{
		DataDir: cfg.DataDir,
		Version: version,
		Agent:   agentClient,
		APIURL:  cfg.GitHubAPIURL,
		Notify:  updateService.Notify,
		Logger:  logger,
	})
	if err != nil {
		return err
	}
	go selfUpdateService.Run(ctx)

	backupService, err := backups.New(backups.Options{
		DataDir:  cfg.DataDir,
		Proxmox:  pve,
		Agent:    agentClient,
		SelfVMID: cfg.SelfVMID,
		Notify:   updateService.Notify,
		Logger:   logger,
	})
	if err != nil {
		return err
	}
	go backupService.RunMonitor(ctx)

	tailscaleService, err := tailscale.New(tailscale.Options{
		DataDir:  cfg.DataDir,
		Backend:  serveBackend(cfg),
		Logger:   logger,
		Services: tailnetServices(),
	})
	if err != nil {
		return err
	}

	authService := auth.NewService(admin)
	if err := authService.PersistTo(filepath.Join(cfg.DataDir, "sessions.json")); err != nil {
		return err
	}

	appsService := apps.New(apps.Options{
		Agent:    agentClient,
		Proxmox:  pve,
		SelfVMID: cfg.SelfVMID,
		Hostname: cfg.Hostname,
		Notify:   updateService.Notify,
		// A Jellyfin that Iglo-OS installed is connected to the Jellyfin page at once.
		OnInstalled: func(ctx context.Context, app string, status agent.AppInstallStatus) error {
			if app == agent.PhotosApp {
				return workloadService.Discover("http://" + status.IP + ":2283")
			}
			if app != agent.JellyfinApp || status.APIKey == "" {
				return nil
			}
			return jellyfinService.SaveSettings(ctx, jellyfin.Settings{URL: "http://" + status.IP + ":8096", APIKey: status.APIKey})
		},
		// The Jellyfin page forgets a Jellyfin that is gone.
		OnRemoved: func(ctx context.Context, app string, ip string) error {
			if app != agent.JellyfinApp {
				return nil
			}
			_, err := jellyfinService.Disconnect(ip)
			return err
		},
		Version: version,
		Logger:  logger,
	})
	// A Moonlight session holds heavy background work, also without Play.
	go appsService.WatchGaming(ctx, workloadService.HoldWhilePlaying)
	appProxy := appproxy.New(cfg.Hostname, []appproxy.App{
		{Name: "seerr", Title: "Seerr", Resolve: func(ctx context.Context) (string, error) {
			return appsService.Address(ctx, agent.MediaStackApp, 5055)
		}},
		{Name: "jellyfin", Title: "Jellyfin", Resolve: func(context.Context) (string, error) {
			if url := jellyfinService.Settings().URL; url != "" {
				return url, nil
			}
			return "", errors.New("connect Jellyfin on the Jellyfin page of Iglo-OS first")
		}},
	}, logger)

	handler := server.New(server.Options{
		Auth:       authService,
		Proxmox:    pve,
		Agent:      agentClient,
		Updates:    updateService,
		Jellyfin:   jellyfinService,
		Health:     healthService,
		SelfUpdate: selfUpdateService,
		Tailscale:  tailscaleService,
		Backups:    backupService,
		Logs:       agentClient,
		Console:    agentClient,
		Apps:       appsService,
		Workloads:  workloadService,
		// The box art copies start with a dot, so data backups skip them.
		Covers: covers.New(filepath.Join(cfg.DataDir, ".covers")),
		SettingsFile: &settingsfile.Service{
			Updates:    updateService,
			Health:     healthService,
			Backups:    backupService,
			Tailscale:  tailscaleService,
			Jellyfin:   jellyfinService,
			SelfUpdate: selfUpdateService,
			Workloads:  workloadService,
		},
		DataDir:       cfg.DataDir,
		Restart:       func() { restart(errRestart) },
		Notify:        updateService.Notify,
		Background:    ctx,
		Web:           webFS,
		SecureCookies: !cfg.Dev,
		Logger:        logger,
	})

	// Requests for app names like seerr.iglo.local go to the app, before
	// the checks of Iglo-OS's own pages.
	handler = appProxy.Handler(handler)

	// Tailscale Services forward to these, one listener per app.
	servers := []*http.Server{}
	for _, app := range tailnetApps {
		servers = append(servers, newServer(app.addr, appProxy.App(app.name)))
	}

	if cfg.Dev {
		logger.Warn("dev mode: serving plain HTTP", "addr", cfg.HTTPAddr)
		return stopReason(restartCtx, runAll(ctx, append(servers, newServer(cfg.HTTPAddr, handler))))
	}

	authority, err := tlsca.Load(filepath.Join(cfg.DataDir, "tls"), cfg.Hostname, appProxy.Hostnames()...)
	if err != nil {
		return err
	}

	httpsServer := newServer(cfg.HTTPSAddr, handler)
	httpsServer.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: authority.GetCertificate}

	httpServer := newServer(cfg.HTTPAddr, redirectHandler(cfg.Hostname, appProxy.IsAppHost, authority.CertPEM()))

	logger.Info("listening", "https", cfg.HTTPSAddr, "http", cfg.HTTPAddr, "hostname", cfg.Hostname, "version", version)

	return stopReason(restartCtx, runAll(ctx, append(servers, httpsServer, httpServer)))
}

// tailnetApps are the apps that can be Tailscale Services, like
// https://seerr.<tailnet>.ts.net. Their listeners only take local connections.
var tailnetApps = []struct{ name, title, addr string }{
	{"seerr", "Seerr", "127.0.0.1:18081"},
	{"jellyfin", "Jellyfin", "127.0.0.1:18082"},
}

// admissionRefresh is how often a worker gets an unchanged answer again, in
// case it was reinstalled with a fresh answer while it kept the same ID.
const admissionRefresh = 10 * time.Minute

// admissionSender tells the running worker containers whether they may start
// new work. Each answer is a pct call on the host, so a fixed answer (-1 or 0)
// only goes out when it changes. A lease always goes out, because it expires.
// Only the workloads loop calls it, one call at a time.
func admissionSender(resources func(context.Context) ([]proxmox.Resource, error), send func(context.Context, string, int, int64) error, now func() time.Time) func(context.Context, string, int64) error {
	type answer struct {
		until  int64
		sentAt time.Time
	}
	sent := map[string]answer{}
	return func(ctx context.Context, node string, until int64) error {
		all, err := resources(ctx)
		if err != nil {
			return err
		}
		seen := map[string]bool{}
		var failures []error
		for _, r := range all {
			if r.Type != "lxc" || r.Status != "running" || (until != -1 && r.Node != node) {
				continue
			}
			tags := strings.Split(r.Tags, ";")
			if !slices.Contains(tags, "iglo") {
				continue
			}
			for _, app := range []string{agent.MusicApp, agent.InboxApp} {
				if !slices.Contains(tags, app) {
					continue
				}
				key := app + "/" + strconv.Itoa(r.VMID)
				seen[key] = true
				if last, ok := sent[key]; ok && until <= 0 && last.until == until && now().Sub(last.sentAt) < admissionRefresh {
					continue
				}
				if err := send(ctx, app, r.VMID, until); err != nil {
					delete(sent, key)
					failures = append(failures, err)
					continue
				}
				sent[key] = answer{until: until, sentAt: now()}
			}
		}
		// A worker that stopped gets its answer again when it runs again.
		maps.DeleteFunc(sent, func(key string, _ answer) bool { return !seen[key] })
		return errors.Join(failures...)
	}
}

func tailnetServices() []tailscale.AppService {
	services := make([]tailscale.AppService, 0, len(tailnetApps))
	for _, app := range tailnetApps {
		services = append(services, tailscale.AppService{Name: app.name, Title: app.title, Backend: "http://" + app.addr})
	}

	return services
}

// runAll runs the servers until ctx ends or one of them fails.
func runAll(ctx context.Context, servers []*http.Server) error {
	group, ctx := errgroup.WithContext(ctx)
	for _, srv := range servers {
		group.Go(func() error { return run(ctx, srv) })
	}

	return group.Wait()
}

// errRestart makes the process exit with an error, so systemd starts it again.
var errRestart = errors.New("restarting to load the restored data backup")

func stopReason(ctx context.Context, err error) error {
	if err == nil && errors.Is(context.Cause(ctx), errRestart) {
		return errRestart
	}

	return err
}

// serveBackend is the local address that Tailscale Serve forwards to. The
// manager's certificate is only for its LAN name, so Serve must not check it.
func serveBackend(cfg config.Config) string {
	if cfg.Dev {
		return "http://127.0.0.1:" + port(cfg.HTTPAddr)
	}

	return "https+insecure://127.0.0.1:" + port(cfg.HTTPSAddr)
}

func port(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}

	return port
}

// redirectHandler sends everything to HTTPS, except the CA certificate that
// devices need to download before they can trust HTTPS. App names keep their
// name. Any other host goes to the hostname, so this is no open redirect.
func redirectHandler(hostname string, isAppHost func(string) bool, caPEM []byte) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ca.crt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-x509-ca-cert")
		w.Header().Set("Content-Disposition", `attachment; filename="iglo-ca.crt"`)
		w.Write(caPEM)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		host := hostname
		if isAppHost(r.Host) {
			host = strings.ToLower(r.Host)
			if name, _, err := net.SplitHostPort(host); err == nil {
				host = name
			}
		}
		http.Redirect(w, r, fmt.Sprintf("https://%s%s", host, r.URL.RequestURI()), http.StatusMovedPermanently)
	})

	return mux
}

func newServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
}

func run(ctx context.Context, srv *http.Server) error {
	errs := make(chan error, 1)
	go func() {
		if srv.TLSConfig != nil {
			errs <- srv.ListenAndServeTLS("", "")
		} else {
			errs <- srv.ListenAndServe()
		}
	}()

	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		return srv.Shutdown(shutdownCtx)
	}
}
