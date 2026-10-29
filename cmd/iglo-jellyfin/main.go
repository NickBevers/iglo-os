// iglo-jellyfin does the first setup of a new Jellyfin server, and adds the
// libraries that are missing on an update. The Jellyfin installer runs it on
// the Proxmox host.
//
//	iglo-jellyfin setup --url http://192.168.1.20:8096 --admin dries \
//	    --movies /data/media/movies --series /data/media/series [library flags] \
//	    [--gpu [--vainfo FILE] [--huc]] [--theme] < password
//	iglo-jellyfin libraries --url http://192.168.1.20:8096 [library flags] < api-key
//
// The library flags are --music, --music-videos and --home-videos, each with
// the folder as Jellyfin sees it. The last line that setup prints is the API
// key for Iglo-OS:
//
//	IGLO jellyfin-key <key>
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"iglo/internal/stacks/jellyfinsetup"
)

const usage = `usage: iglo-jellyfin setup --url URL --admin NAME --movies PATH --series PATH [library flags] [--gpu [--vainfo FILE] [--huc]] [--theme] < password
       iglo-jellyfin libraries --url URL [library flags] < api-key
library flags: [--music PATH] [--music-videos PATH] [--home-videos PATH]`

func main() {
	run := map[string]func([]string) error{"setup": setup, "libraries": libraries}
	if len(os.Args) < 2 || run[os.Args[1]] == nil {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}

	if err := run[os.Args[1]](os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// extraLibraries are the flags of the libraries that setup and libraries share.
type extraLibraries struct {
	music, musicVideos, homeVideos *string
}

func addExtraLibraries(flags *flag.FlagSet) extraLibraries {
	return extraLibraries{
		music:       flags.String("music", "", "folder of the Music library, as Jellyfin sees it (optional)"),
		musicVideos: flags.String("music-videos", "", "folder of the Music Videos library, as Jellyfin sees it (optional)"),
		homeVideos:  flags.String("home-videos", "", "folder of the Home Videos library, as Jellyfin sees it (optional)"),
	}
}

func (e extraLibraries) apply(cfg *jellyfinsetup.Config) {
	cfg.MusicPath, cfg.MusicVideosPath, cfg.HomeVideosPath = *e.music, *e.musicVideos, *e.homeVideos
}

// readSecret reads the first line of stdin. Secrets come on stdin, so they
// never land in a file or the process list.
func readSecret(what string) (string, error) {
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("pass the %s on stdin", what)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func logf(format string, args ...any) {
	fmt.Printf("==> "+format+"\n", args...)
}

// libraries adds the libraries that are missing to a Jellyfin that is set up.
func libraries(args []string) error {
	flags := flag.NewFlagSet("libraries", flag.ExitOnError)
	url := flags.String("url", "", "address of Jellyfin")
	extra := addExtraLibraries(flags)
	flags.Parse(args)

	if *url == "" {
		return errors.New("--url is required")
	}
	key, err := readSecret("API key")
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg := jellyfinsetup.Config{URL: *url, APIKey: key, Logf: logf}
	extra.apply(&cfg)
	return jellyfinsetup.AddLibraries(ctx, cfg)
}

func setup(args []string) error {
	flags := flag.NewFlagSet("setup", flag.ExitOnError)
	url := flags.String("url", "", "address of Jellyfin")
	admin := flags.String("admin", "", "username of the Jellyfin admin")
	movies := flags.String("movies", "", "folder of the Movies library, as Jellyfin sees it")
	series := flags.String("series", "", "folder of the Series library, as Jellyfin sees it")
	anime := flags.String("anime", "", "folder of the Anime library, as Jellyfin sees it (optional)")
	animeMovies := flags.String("anime-movies", "", "folder of the Anime Movies library, as Jellyfin sees it (optional)")
	extra := addExtraLibraries(flags)
	gpu := flags.Bool("gpu", false, "turn on VA-API hardware transcoding")
	vainfo := flags.String("vainfo", "", "file with the output of vainfo, for the codecs that the GPU decodes")
	huc := flags.Bool("huc", false, "the host loaded the HuC firmware, for the Intel low-power encoders")
	theme := flags.Bool("theme", false, "turn on the Netflix theme")
	flags.Parse(args)

	if *url == "" || *admin == "" || *movies == "" || *series == "" {
		return errors.New("--url, --admin, --movies and --series are required")
	}

	password, err := readSecret("admin password")
	if err != nil {
		return err
	}

	gpuInfo := ""
	if *vainfo != "" {
		data, err := os.ReadFile(*vainfo)
		if err != nil {
			return err
		}
		gpuInfo = string(data)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg := jellyfinsetup.Config{
		URL:             *url,
		AdminUsername:   *admin,
		AdminPassword:   password,
		MoviesPath:      *movies,
		SeriesPath:      *series,
		AnimePath:       *anime,
		AnimeMoviesPath: *animeMovies,
		GPU:             *gpu,
		VAInfo:          gpuInfo,
		HuC:             *huc,
		Theme:           *theme,
		Logf:            logf,
	}
	extra.apply(&cfg)
	key, err := jellyfinsetup.Setup(ctx, cfg)
	if err != nil {
		return err
	}

	fmt.Println("IGLO jellyfin-key " + key)

	return nil
}
