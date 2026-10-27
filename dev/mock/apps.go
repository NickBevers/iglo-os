package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"iglo/internal/agent"
)

// failPassword makes a fake install fail, to see the error on the Apps page.
const failPassword = "failfailfail12"

// fakeInstalls plays the installers. The real installer of the agent does
// everything else: checking answers, saving them for a retry, and the status.
type fakeInstalls struct {
	mu      sync.Mutex
	running bool
}

func newAppInstaller() (*agent.AppInstaller, error) {
	stacks := statePath("stacks")
	for _, dir := range []string{"arr", "jellyfin", "minecraft", "photos", "music", "inbox", "gaming"} {
		if err := os.MkdirAll(filepath.Join(stacks, dir), 0o700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(stacks, dir, "install.sh"), nil, 0o600); err != nil {
			return nil, err
		}
	}

	fake := &fakeInstalls{}
	return agent.NewAppInstallerWith(statePath("apps"), stacks, fake.start, fake.isRunning), nil
}

func (f *fakeInstalls) isRunning() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.running
}

func (f *fakeInstalls) start(script string, args []string, cleanup, logPath, exitPath string) error {
	stack := filepath.Base(filepath.Dir(script))
	play := func() { f.playOnContainer(stack, args[0], logPath, exitPath) }
	if args[0] == "--vpn" {
		answers, err := os.ReadFile(cleanup)
		if err != nil {
			return err
		}
		os.Remove(cleanup)
		play = func() { f.playVPN(string(answers), logPath, exitPath) }
	}
	if args[0] == "--players" {
		answers, err := os.ReadFile(cleanup)
		if err != nil {
			return err
		}
		os.Remove(cleanup)
		play = func() { f.playPlayers(string(answers), logPath, exitPath) }
	}
	if args[0] == "--answers" {
		answers, err := os.ReadFile(cleanup)
		if err != nil {
			return err
		}
		os.Remove(cleanup)
		play = func() { f.play(stack, string(answers), logPath, exitPath) }
	}

	f.mu.Lock()
	f.running = true
	f.mu.Unlock()

	go play()
	return nil
}

// playVPN plays a change of the VPN settings. Countries with "Mars" do not connect.
func (f *fakeInstalls) playVPN(answers, logPath, exitPath string) {
	defer func() {
		f.mu.Lock()
		f.running = false
		f.mu.Unlock()
	}()

	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer logFile.Close()
	write := func(line string) {
		fmt.Fprintln(logFile, line)
		time.Sleep(time.Second)
	}

	countries := ""
	for line := range strings.Lines(answers) {
		if value, found := strings.CutPrefix(strings.TrimSpace(line), "VPN_COUNTRIES="); found {
			countries = value
		}
	}

	write("==> Saving the new VPN settings")
	write("==> Restarting the VPN and the apps behind it")
	write("==> Waiting for the VPN")
	if strings.Contains(countries, "Mars") {
		write("Last lines of the VPN log (gluetun):")
		write("ERROR [vpn] no server found for countries: " + countries)
		write("==> The VPN did not connect, going back to the old settings")
		write("==> Restarting the VPN and the apps behind it")
		write("error: the VPN did not connect with the new settings, so the old settings are back")
		os.WriteFile(exitPath, []byte("1\n"), 0o600)
		return
	}
	os.WriteFile(statePath("vpn-countries"), []byte(countries), 0o600)
	write("==> Done")
	os.WriteFile(exitPath, []byte("0\n"), 0o600)
}

// playOnContainer plays an update or a removal. With .dev/fail-update, an update fails.
func (f *fakeInstalls) playOnContainer(stack, action, logPath, exitPath string) {
	defer func() {
		f.mu.Lock()
		f.running = false
		f.mu.Unlock()
	}()

	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer logFile.Close()
	write := func(line string) {
		fmt.Fprintln(logFile, line)
		time.Sleep(time.Second)
	}
	finish := func(code int) { os.WriteFile(exitPath, []byte(fmt.Sprintln(code)), 0o600) }

	installed := map[string]string{"arr": "media-installed", "jellyfin": "jellyfin-installed", "minecraft": "minecraft-installed", "photos": "photos-installed", "music": "music-installed", "inbox": "inbox-installed", "gaming": "gaming-installed"}[stack]
	if action == "--backup" || action == "--verify" {
		write("==> Development simulation: no files copied or database restored")
		write("==> Simulated Photos recovery finished")
		finish(0)
		return
	}
	if action == "--remove" {
		if id := map[string]string{"arr": "130", "jellyfin": "140", "photos": "170", "music": "160", "inbox": "180", "gaming": "190"}[stack]; id != "" {
			os.Remove(statePath("binding-containers/" + id + ".conf"))
		}
		write("==> Shutting down the container")
		write("==> Removing the container with its disks and snapshots. Its backups stay")
		os.Remove(statePath(installed))
		write("==> Done")
		finish(0)
		return
	}

	write("==> Updating the packages in the container")
	switch stack {
	case "photos", "music", "inbox", "gaming":
		write("==> Development simulation: scoped app update")
	case "arr":
		write("==> Copying the stack of Iglo-OS dev into the container")
		write("==> Downloading the new images")
		write("==> Starting the stack")
		write("==> Waiting for the VPN")
	case "minecraft":
		write("==> Copying the stack of Iglo-OS dev into the container")
		write("==> Downloading the new images")
		write("==> Starting the server")
		write("==> Waiting for the server")
	default:
		write("==> Waiting for Jellyfin")
	}
	if _, err := os.Stat(statePath("fail-update")); err == nil {
		write("error: the fake update failed, because .dev/fail-update exists")
		finish(1)
		return
	}
	write("==> Done")
	finish(0)
}

func (f *fakeInstalls) play(stack, answers, logPath, exitPath string) {
	defer func() {
		f.mu.Lock()
		f.running = false
		f.mu.Unlock()
	}()

	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer logFile.Close()
	write := func(line string) {
		fmt.Fprintln(logFile, line)
		time.Sleep(time.Second)
	}
	finish := func(code int) { os.WriteFile(exitPath, []byte(fmt.Sprintln(code)), 0o600) }

	steps := []string{"==> Mounting the media", "==> Downloading Debian 13 template"}
	switch stack {
	case "photos", "music", "inbox":
		steps = []string{"==> Development simulation: no host or NAS changes", "==> Checking scoped storage bindings", "==> Creating isolated app container", "==> Starting the stack"}
	case "gaming":
		steps = []string{"==> Development simulation: no host or NAS changes", "==> Checking the Games share, GPU and input devices",
			"==> Creating the privileged gaming container", "==> Installing Docker", "==> Starting Wolf"}
	case "minecraft":
		steps = []string{"==> Downloading Debian 13 template", "==> Creating container 150 (minecraft) with 5 GB memory", "==> Installing Docker",
			"==> Copying the stack into the container", "==> Starting the server (the first start downloads Paper and makes the world, this takes a few minutes)",
			"==> Waiting for the server"}
	case "jellyfin":
		steps = append(steps, "==> Creating container 140 (jellyfin)", "==> Installing Jellyfin from repo.jellyfin.org",
			"==> Passing the GPU (/dev/dri/renderD128) to the container", "==> Setting up Jellyfin", "==> Finishing the setup wizard")
	default:
		steps = append(steps, "==> Creating container 130 (media)", "==> Installing Docker",
			"==> Starting the stack (the first image download takes a few minutes)", "==> Waiting for the VPN",
			"==> Configuring anime", "==> Syncing the quality profiles with Recyclarr")
		if !strings.Contains(answers, "JELLYFIN_ADMIN_USERNAME=\n") {
			steps = append(steps, "==> Configuring Seerr", "==> Anime films use the [Anime] Remux-1080p profile")
		}
	}
	for _, step := range steps {
		write(step)
	}

	if strings.Contains(answers, "PASSWORD="+failPassword+"\n") {
		write("error: the fake install failed, because of the password " + failPassword)
		write("==> The install failed, removing the container")
		finish(1)
		return
	}

	if stack == "arr" || stack == "jellyfin" {
		if err := bindVideos(map[string]int{"arr": 130, "jellyfin": 140}[stack], answers); err != nil {
			finish(1)
			return
		}
	}
	switch stack {
	case "photos", "music", "inbox", "gaming":
		id := map[string]int{"music": 160, "photos": 170, "inbox": 180, "gaming": 190}[stack]
		if err := os.MkdirAll(statePath("binding-containers"), 0700); err != nil {
			finish(1)
			return
		}
		conf := ""
		n := 0
		for line := range strings.Lines(answers) {
			key, value, _ := strings.Cut(line, "=")
			if (key == "LIBRARY_BINDING" || key == "OUTPUT_BINDING" || key == "BACKUP_BINDING") && value != "" {
				conf += fmt.Sprintf("mp%d: %s,mp=/data/%s\n", n, statePath("shares/"+value), key)
				n++
			}
		}
		if err := os.WriteFile(statePath(fmt.Sprintf("binding-containers/%d.conf", id)), []byte(conf), 0600); err != nil {
			finish(1)
			return
		}
		ip := fmt.Sprintf("192.168.1.%d", id)
		// The fake Immich runs here, so the setup can create its admin.
		if stack == "photos" {
			ip = "127.0.0.1"
		}
		write(fmt.Sprintf("IGLO app %s %d %s", stack, id, ip))
		os.WriteFile(statePath(stack+"-installed"), nil, 0600)
	case "minecraft":
		savePlayers(answers)
		write("==> Done")
		write("IGLO app minecraft 150 192.168.1.170")
		os.WriteFile(statePath("minecraft-installed"), nil, 0o600)
	case "jellyfin":
		write("IGLO jellyfin-key 0123456789abcdef0123456789abcdef")
		write("==> Done")
		write("IGLO app jellyfin 140 192.168.1.160")
		os.WriteFile(statePath("jellyfin-installed"), nil, 0o600)
	default:
		write("==> Done")
		write("IGLO app media 130 192.168.1.150")
		os.WriteFile(statePath("media-installed"), nil, 0o600)
	}
	finish(0)
}

// bindVideos records that a fake Jellyfin or media stack uses the Videos
// share of its answers, so the Storage page lists it as a consumer.
func bindVideos(id int, answers string) error {
	for line := range strings.Lines(answers) {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "VIDEOS_BINDING="); ok && value != "" {
			if err := os.MkdirAll(statePath("binding-containers"), 0o700); err != nil {
				return err
			}
			conf := fmt.Sprintf("mp0: %s,mp=/data/media\n", statePath("shares/"+value))
			return os.WriteFile(statePath(fmt.Sprintf("binding-containers/%d.conf", id)), []byte(conf), 0o600)
		}
	}
	return nil
}

// savePlayers keeps the players of a fake Minecraft install: the operator is on the whitelist too.
func savePlayers(answers string) {
	var operator, whitelist string
	for line := range strings.Lines(answers) {
		key, value, _ := strings.Cut(strings.TrimSpace(line), "=")
		switch key {
		case "OPERATOR":
			operator = value
		case "WHITELIST":
			whitelist = value
		}
	}

	players := agent.MinecraftPlayers{Whitelist: []string{operator}, Operators: []string{operator}}
	for name := range strings.SplitSeq(whitelist, ",") {
		if name != "" {
			players.Whitelist = append(players.Whitelist, name)
		}
	}
	data, _ := json.Marshal(players)
	os.WriteFile(statePath("minecraft-players.json"), data, 0o600)
}

func loadPlayers() agent.MinecraftPlayers {
	players := agent.MinecraftPlayers{Whitelist: []string{"Steve"}, Operators: []string{"Steve"}}
	if data, err := os.ReadFile(statePath("minecraft-players.json")); err == nil {
		json.Unmarshal(data, &players)
	}

	return players
}

// playPlayers plays a change of the players. A name with "Ghost" in it has no Java profile.
func (f *fakeInstalls) playPlayers(answers, logPath, exitPath string) {
	defer func() {
		f.mu.Lock()
		f.running = false
		f.mu.Unlock()
	}()

	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer logFile.Close()

	var players agent.MinecraftPlayers
	for line := range strings.Lines(answers) {
		key, value, _ := strings.Cut(strings.TrimSpace(line), "=")
		names := strings.FieldsFunc(value, func(r rune) bool { return r == ',' })
		switch key {
		case "WHITELIST":
			players.Whitelist = names
		case "OPS":
			players.Operators = names
		}
	}

	time.Sleep(time.Second)
	for _, name := range players.Whitelist {
		if strings.Contains(name, "Ghost") {
			fmt.Fprintf(logFile, "error: Minecraft does not know the player %s. Check the Java profile name on minecraft.net\n", name)
			os.WriteFile(exitPath, []byte("1\n"), 0o600)
			return
		}
	}
	data, _ := json.Marshal(players)
	os.WriteFile(statePath("minecraft-players.json"), data, 0o600)
	fmt.Fprintln(logFile, "==> Done")
	os.WriteFile(exitPath, []byte("0\n"), 0o600)
}

// addAppRoutes serves the app routes of the agent with the real installer.
func addAppRoutes(mux *http.ServeMux, installer *agent.AppInstaller) {
	agent.RegisterEvents(mux, installer)
	for _, action := range []agent.AppAction{agent.ActionBackup, agent.ActionVerify} {
		mux.HandleFunc("POST /v1/apps/photos/"+string(action), func(w http.ResponseWriter, r *http.Request) {
			var in agent.AppContainerRequest
			if json.NewDecoder(r.Body).Decode(&in) != nil {
				http.Error(w, "invalid request", 400)
				return
			}
			if err := installer.Recover(agent.PhotosApp, in.VMID, action); err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			w.WriteHeader(202)
		})
	}
	fail := func(w http.ResponseWriter, err error) {
		switch {
		case errors.Is(err, agent.ErrInvalidAnswers), errors.Is(err, agent.ErrUnknownApp), errors.Is(err, agent.ErrNoSavedAnswers):
			http.Error(w, err.Error(), http.StatusBadRequest)
		case errors.Is(err, agent.ErrAppInstallRunning):
			http.Error(w, err.Error(), http.StatusConflict)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}

	mux.HandleFunc("GET /v1/apps/install", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, installer.Status())
	})
	mux.HandleFunc("POST /v1/apps/{app}/install", func(w http.ResponseWriter, r *http.Request) {
		var request agent.InstallRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if err := installer.Install(r.PathValue("app"), request); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})
	mux.HandleFunc("POST /v1/apps/{app}/retry", func(w http.ResponseWriter, r *http.Request) {
		if err := installer.Retry(r.PathValue("app")); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})
	for action, run := range map[string]func(string, int) error{"update": installer.Update, "remove": installer.Remove} {
		mux.HandleFunc("POST /v1/apps/{app}/"+action, func(w http.ResponseWriter, r *http.Request) {
			var request agent.AppContainerRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				http.Error(w, "invalid request body", http.StatusBadRequest)
				return
			}
			if err := run(r.PathValue("app"), request.VMID); err != nil {
				fail(w, err)
				return
			}
			w.WriteHeader(http.StatusAccepted)
		})
	}
	mux.HandleFunc("GET /v1/apps/media/vpn", func(w http.ResponseWriter, r *http.Request) {
		countries := "Netherlands"
		if data, err := os.ReadFile(statePath("vpn-countries")); err == nil {
			countries = string(data)
		}
		writeJSON(w, agent.VPNSettings{Countries: countries})
	})
	mux.HandleFunc("PUT /v1/apps/media/vpn", func(w http.ResponseWriter, r *http.Request) {
		var request agent.VPNRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if err := installer.ChangeVPN(request.VMID, request.VPNSettings); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})
	mux.HandleFunc("GET /v1/apps/minecraft/players", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, loadPlayers())
	})
	mux.HandleFunc("PUT /v1/apps/minecraft/players", func(w http.ResponseWriter, r *http.Request) {
		var request agent.PlayersRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if err := installer.ChangePlayers(request.VMID, request.MinecraftPlayers); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})
	mux.HandleFunc("GET /v1/apps/{app}/stats", serveStats)
	mux.HandleFunc("GET /v1/apps/{app}/answers", func(w http.ResponseWriter, r *http.Request) {
		saved, err := installer.Saved(r.PathValue("app"))
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, saved)
	})
	mux.HandleFunc("DELETE /v1/apps/{app}/answers", func(w http.ResponseWriter, r *http.Request) {
		if err := installer.Forget(r.PathValue("app")); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
