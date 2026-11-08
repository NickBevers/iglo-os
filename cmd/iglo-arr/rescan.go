package main

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"iglo/internal/stacks/arr"
)

func libraryCommand(args []string, check bool) error {
	flags := flag.NewFlagSet("library", flag.ContinueOnError)
	envPath := flags.String("env", "/opt/arr/.env", "stack settings file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	paths := flags.Args()
	if err := arr.ValidateLibraryPaths(paths); err != nil {
		return err
	}
	env, err := readEnv(*envPath, "SONARR_API_KEY", "RADARR_API_KEY")
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cfg := arr.RescanConfig{Endpoints: arr.DefaultEndpoints, SonarrAPIKey: env["SONARR_API_KEY"], RadarrAPIKey: env["RADARR_API_KEY"]}
	var result any
	if check {
		result, err = arr.ShrinkCheck(ctx, paths, cfg)
	} else {
		result, err = arr.Rescan(ctx, paths, cfg)
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}
func rescan(args []string) error      { return libraryCommand(args, false) }
func shrinkCheck(args []string) error { return libraryCommand(args, true) }
