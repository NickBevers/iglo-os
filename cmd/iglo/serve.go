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
	"iglo/internal/events"
	"iglo/internal/health"
	"iglo/internal/immich"
	"iglo/internal/jellyfin"
	"iglo/internal/proxmox"
	"iglo/internal/selfupdate"
	"iglo/internal/server"
	"iglo/internal/settingsfile"
	"iglo/internal/setup"
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
	// hub tells the open pages what changed, so they don't wait for their poll.
	hub := events.NewHub()
	workloadService, err := workloads.New(workloads.Options{DataDir: cfg.DataDir, WorkerAdmission: admissionSender(pve.Resources, agentClient.WorkerAdmission, time.Now), Changed: hub.Publish, HostMemory: hostMemory(agentClient)})
	if err != nil {
		return err
	}
	go workloadService.Run(ctx)
	updateService, err := updates.New(updates.Options{
		Changed:  hub.Publish,
		DataDir:  cfg.DataDir,
		Proxmox:  pve,
		Agent:    agentClient,
		SelfVMID: cfg.SelfVMID,
		Logger:   logger,
	})
	if err != nil {
		return err
	}
	go updateService.RunNotifications(ctx)
	go updateService.RunScheduler(ctx)

	jellyfinService, err := jellyfin.NewService(cfg.DataDir)
	if err != nil {
		return err
	}

	healthService, err := health.New(health.Options{
		Changed: hub.Publish,
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
	})
	if err != nil {
		return err
	}

	backupService, err := backups.New(backups.Options{
		Changed:  hub.Publish,
		DataDir:  cfg.DataDir,
		Proxmox:  pve,
		Agent:    agentClient,
		SelfVMID: cfg.SelfVMID,
		Notify:   updateService.Notify,
		Logger:   logger,
		// The monthly restore check waits while a game runs, like other heavy work.
		Hold: func() bool { return workloadService.Status().GamingActive },
	})
	if err != nil {
		return err
	}
	go backupService.RunMonitor(ctx)
	// Heavy background work waits while backups or updates run. The window
	// of background work overlaps the nightly backups (03:00), the weekly
	// updates (04:00) and the restore check (05:00).
	// backupsOrUpdates also keeps the nightly Photos backup from starting.
	backupsOrUpdates := func(ctx context.Context) string {
		switch {
		case backupService.Running(ctx):
			return "Backups are running"
		case updateService.Running():
			return "Updates are running"
		}
		return ""
	}

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
				return workloadService.Discover(photosAddress(status))
			}
			if app != agent.JellyfinApp || status.APIKey == "" {
				return nil
			}
			return jellyfinService.SaveSettings(ctx, jellyfin.Settings{URL: "http://" + status.IP + ":8096", APIKey: status.APIKey})
		},
		// The update of Jellyfin adds the libraries that are missing with it.
		JellyfinKey: func() string { return jellyfinService.Settings().APIKey },
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
		Changed: hub.Publish,
	})
	// The Photos backup holds heavy work too: it reads the whole library.
	workloadService.SetHold(func(ctx context.Context) string {
		if reason := appsService.PhotosBackupRunning(ctx); reason != "" {
			return reason
		}
		return backupsOrUpdates(ctx)
	})
	// The nightly Photos backup, at 01:00 by default: before the processing
	// window of background work and the Proxmox backups.
	go appsService.RunPhotosBackups(ctx, backupsOrUpdates)
	// A Moonlight session holds heavy background work, also without Play.
	go appsService.WatchGaming(ctx, workloadService.HoldWhilePlaying)
	go appsService.WatchMusic(ctx)
	go appsService.WatchInbox(ctx)
	go jellyfinService.Watch(ctx, func() { hub.Publish(events.Jellyfin) })
	go followAgent(ctx, agentClient, hub, appsService)
	setupService := setup.New(setup.Options{
		Apps: appsService, Agent: agentClient, Backups: backupService, Workloads: workloadService,
		JellyfinKey: func() string { return jellyfinService.Settings().APIKey },
		Changed:     func() { hub.Publish(events.Setup) },
		Background:  ctx,
		DataDir:     cfg.DataDir,
		Logger:      logger,
	})
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
		// A Photos from the Apps page connects Background work by itself, like the setup.
		ConnectPhotos: func(ctx context.Context, status agent.AppInstallStatus, admin immich.Admin) error {
			return workloadService.ConnectInstalled(ctx, photosAddress(status), admin)
		},
		Events: hub,
		Setup:  setupService,
		// The box art copies start with a dot, so data backups skip them.
		Covers: covers.New(filepath.Join(cfg.DataDir, ".covers")),
		SettingsFile: &settingsfile.Service{
			Updates:   updateService,
			Health:    healthService,
			Backups:   backupService,
			Tailscale: tailscaleService,
			Jellyfin:  jellyfinService,
			Workloads: workloadService,
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

// photosAddress is the Immich of a Photos that Iglo-OS installed.
func photosAddress(status agent.AppInstallStatus) string {
	return "http://" + status.IP + ":2283"
}

// hostMemory reads the memory of the host from the host agent, for the
// checks of Background work.
func hostMemory(client *agent.Client) func(context.Context) (workloads.HostMemory, error) {
	return func(ctx context.Context) (workloads.HostMemory, error) {
		memory, err := client.HostMemory(ctx)
		if err != nil {
			return workloads.HostMemory{}, err
		}
		return workloads.HostMemory{Node: memory.Node, Total: memory.TotalBytes, Available: memory.AvailableBytes, Pressure: memory.Pressure}, nil
	}
}

// followAgent passes the events of the host agent on. An install, update or
// removal that moves on wakes its monitor and the Apps page.
func followAgent(ctx context.Context, client *agent.Client, hub *events.Hub, apps *apps.Service) {
	for ctx.Err() == nil {
		client.Events(ctx, func(topic string) {
			if topic == events.Apps {
				apps.Wake()
				hub.Publish(events.Apps)
			}
		})
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
		}
	}
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

// leaseMargin is how long, in seconds, a sent lease must still run to skip
// sending a new one. The loop checks every 15 seconds and a lease lasts a
// minute, so a lease goes out about every 30 seconds, with 30 to spare.
const leaseMargin = 40

// admissionSender tells the running worker containers whether they may start
// new work. Each answer is a pct call on the host, so a fixed answer (-1 or 0)
// only goes out when it changes. A lease always goes out, because it expires.
// Only the workloads loop calls it, one call at a time.
func admissionSender(resources func(context.Context) ([]proxmox.Resource, error), send func(context.Context, string, int, int64) error, now func() time.Time) func(context.Context, int64) error {
	type answer struct {
		until  int64
		sentAt time.Time
	}
	sent := map[string]answer{}
	return func(ctx context.Context, until int64) error {
		all, err := resources(ctx)
		if err != nil {
			return err
		}
		seen := map[string]bool{}
		var failures []error
		for _, r := range all {
			// Iglo-OS runs on one host, so every running worker is on it.
			if r.Type != "lxc" || r.Status != "running" {
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
				// A lease that still runs for a while goes out every other check,
				// not every check: each one is a pct exec on the host.
				if last, ok := sent[key]; ok && until > 0 && last.until > 0 && last.until-now().Unix() >= leaseMargin {
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
