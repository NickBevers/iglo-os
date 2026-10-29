// iglo-arr runs inside the media LXC and connects the apps of the arr stack.
//
//	iglo-arr configure --env /opt/arr/.env < passwords
//	iglo-arr vpn --env /opt/arr/.env < settings
//	iglo-arr flaresolverr --env /opt/arr/.env on|off
//
// For configure, the first line on stdin is the password for the apps.
// Optional next lines are the password of the Jellyfin admin, for the setup
// of Seerr, and the password of the OpenSubtitles.com account, for Bazarr.
//
// For vpn, stdin has KEY=value lines: VPN_PROVIDER, VPN_LOCATIONS (separated
// by commas), and WIREGUARD_PRIVATE_KEY, PIA_USERNAME, PIA_PASSWORD and
// PIA_DEDICATED_TOKEN. It writes the settings without secrets in .env, and
// prints the secret files for Gluetun as name=value lines, for the host to
// seal. The secrets never land on the disk of the container.
//
// flaresolverr adds the FlareSolverr proxy to Prowlarr or removes it, and
// writes the choice in .env. It needs no password: Prowlarr's API key is in
// .env. The installer starts or stops the service itself.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"iglo/internal/stacks/arr"
)

func main() {
	commands := map[string]func([]string) error{"configure": configure, "vpn": applyVPN, "flaresolverr": setFlareSolverr}
	if len(os.Args) < 2 || commands[os.Args[1]] == nil {
		fmt.Fprintln(os.Stderr, "usage: iglo-arr configure [--env FILE] [--data-dir DIR] [--bazarr-config FILE] < passwords")
		fmt.Fprintln(os.Stderr, "       iglo-arr vpn [--env FILE] [--gluetun-dir DIR] < settings")
		fmt.Fprintln(os.Stderr, "       iglo-arr flaresolverr [--env FILE] on|off")
		os.Exit(2)
	}

	if err := commands[os.Args[1]](os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// applyVPN writes the VPN settings from stdin in .env, and prints Gluetun's
// secret files. The secrets come on stdin and go out on stdout, so they never
// land in the process list or a file.
func applyVPN(args []string) error {
	flags := flag.NewFlagSet("vpn", flag.ExitOnError)
	envPath := flags.String("env", "/opt/arr/.env", "stack .env file")
	gluetunDir := flags.String("gluetun-dir", "/opt/arr/config/gluetun", "folder that Gluetun sees as /gluetun")
	flags.Parse(args)

	settings := map[string]string{}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		if key, value, found := strings.Cut(scanner.Text(), "="); found {
			settings[key] = value
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(*gluetunDir, 0o700); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	secrets, err := arr.ApplyVPN(ctx, arr.VPN{
		Provider:          settings["VPN_PROVIDER"],
		Locations:         strings.Split(settings["VPN_LOCATIONS"], ","),
		WireGuardKey:      settings["WIREGUARD_PRIVATE_KEY"],
		PIAUsername:       settings["PIA_USERNAME"],
		PIAPassword:       settings["PIA_PASSWORD"],
		PIADedicatedToken: settings["PIA_DEDICATED_TOKEN"],
	}, *envPath, *gluetunDir, arr.NewPIAClient())
	if err != nil {
		return err
	}
	for _, name := range arr.SecretFiles {
		fmt.Printf("%s=%s\n", name, secrets[name])
	}

	return nil
}

// setFlareSolverr turns FlareSolverr on or off in Prowlarr and .env.
func setFlareSolverr(args []string) error {
	flags := flag.NewFlagSet("flaresolverr", flag.ExitOnError)
	envPath := flags.String("env", "/opt/arr/.env", "stack .env file")
	flags.Parse(args)
	if flags.NArg() != 1 || (flags.Arg(0) != "on" && flags.Arg(0) != "off") {
		return errors.New("say on or off")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return arr.SetFlareSolverr(ctx, flags.Arg(0) == "on", *envPath, arr.DefaultEndpoints.Prowlarr, func(format string, args ...any) {
		fmt.Printf("==> "+format+"\n", args...)
	})
}

func configure(args []string) error {
	flags := flag.NewFlagSet("configure", flag.ExitOnError)
	envPath := flags.String("env", "/opt/arr/.env", "stack .env file")
	dataDir := flags.String("data-dir", "/data", "data folder with media/ and downloads/")
	bazarrConfig := flags.String("bazarr-config", "/opt/arr/config/bazarr/config/config.yaml", "Bazarr config file")
	flags.Parse(args)

	env, err := readEnv(*envPath)
	if err != nil {
		return err
	}

	// The passwords come on stdin so they never land in a file or process list.
	stdin := bufio.NewReader(os.Stdin)
	password, err := stdin.ReadString('\n')
	if err != nil && password == "" {
		return errors.New("pass the admin password on stdin")
	}
	password = strings.TrimRight(password, "\r\n")
	jellyfinAdminPassword, _ := stdin.ReadString('\n')
	jellyfinAdminPassword = strings.TrimRight(jellyfinAdminPassword, "\r\n")
	openSubtitlesPassword, _ := stdin.ReadString('\n')
	openSubtitlesPassword = strings.TrimRight(openSubtitlesPassword, "\r\n")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err = arr.Configure(ctx, arr.Config{
		Username:              env["ARR_USERNAME"],
		Password:              password,
		RadarrAPIKey:          env["RADARR_API_KEY"],
		SonarrAPIKey:          env["SONARR_API_KEY"],
		ProwlarrAPIKey:        env["PROWLARR_API_KEY"],
		JellyfinURL:           env["JELLYFIN_URL"],
		JellyfinInternalURL:   env["JELLYFIN_INTERNAL_URL"],
		JellyfinAPIKey:        env["JELLYFIN_API_KEY"],
		JellyfinAdminUsername: env["JELLYFIN_ADMIN_USERNAME"],
		JellyfinAdminPassword: jellyfinAdminPassword,
		OpenSubtitlesUsername: env["OPENSUBTITLES_USERNAME"],
		OpenSubtitlesPassword: openSubtitlesPassword,
		SubtitleLanguages:     strings.Split(env["SUBTITLE_LANGUAGES"], ","),
		MoviesFolder:          env["MOVIES_FOLDER"],
		SeriesFolder:          env["SERIES_FOLDER"],
		AnimeFolder:           env["ANIME_FOLDER"],
		AnimeMoviesFolder:     env["ANIME_MOVIES_FOLDER"],
		// Stacks from before FlareSolverr was optional have no answer: they run it.
		FlareSolverr:     env["FLARESOLVERR"] != "n",
		AnimeIndexers:    env["ANIME_INDEXERS"] == "y",
		DataDir:          *dataDir,
		BazarrConfigPath: *bazarrConfig,
		Endpoints:        arr.DefaultEndpoints,
		QBittorrentTempPassword: func(ctx context.Context) (string, error) {
			return qbittorrentTempPassword(ctx)
		},
		SyncProfiles: syncProfiles,
		Logf: func(format string, args ...any) {
			fmt.Printf("==> "+format+"\n", args...)
		},
	})
	if err != nil {
		return err
	}

	fmt.Println("==> Done")

	return nil
}

// readEnv parses the simple KEY=VALUE lines Docker Compose reads too.
func readEnv(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	env := map[string]string{}
	for line := range strings.Lines(string(data)) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if key, value, found := strings.Cut(line, "="); found {
			env[key] = value
		}
	}

	for _, key := range []string{"ARR_USERNAME", "RADARR_API_KEY", "SONARR_API_KEY", "PROWLARR_API_KEY", "SUBTITLE_LANGUAGES"} {
		if env[key] == "" {
			return nil, fmt.Errorf("%s is missing in %s", key, path)
		}
	}

	return env, nil
}

// syncProfiles runs Recyclarr once, instead of waiting for its daily run.
// Checked against Recyclarr v8.8.0: its CLI is on the PATH of the container.
func syncProfiles(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	output, err := exec.CommandContext(ctx, "docker", "exec", "recyclarr", "recyclarr", "sync").CombinedOutput()
	if err != nil {
		return fmt.Errorf("recyclarr sync: %w: %s", err, lastLines(string(output), 5))
	}

	return nil
}

// lastLines returns the last n lines of output, for an error message.
func lastLines(output string, n int) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")

	return strings.Join(lines[max(0, len(lines)-n):], "\n")
}

// qbittorrentTempPassword reads the one-time password qBittorrent logs on first start.
func qbittorrentTempPassword(ctx context.Context) (string, error) {
	output, err := exec.CommandContext(ctx, "docker", "logs", "qbittorrent").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker logs qbittorrent: %w", err)
	}

	const marker = "temporary password is provided for this session: "
	password := ""
	for line := range strings.Lines(string(output)) {
		if _, after, found := strings.Cut(line, marker); found {
			password = strings.TrimSpace(after)
		}
	}
	if password == "" {
		return "", errors.New("qbittorrent: no temporary password in logs")
	}

	return password, nil
}
