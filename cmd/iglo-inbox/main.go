package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"iglo/internal/inbox"
	"iglo/internal/shrink"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	service, err := inbox.New("/var/lib/iglo-inbox/worker", "/data/inbox", "/data/videos/home-videos")
	if err != nil {
		log.Fatal(err)
	}
	service.AdmissionDir = "/var/lib/iglo-inbox/admission"
	library, err := shrink.New("/var/lib/iglo-inbox/worker/shrink", "/data/videos")
	if err != nil {
		log.Fatal(err)
	}
	library.AdmissionDir = service.AdmissionDir
	encoder := shrink.NewEncoder()
	encoder.DetectGPU(ctx)
	library.Inspect, library.Encode = encoder.Probe, encoder.Encode
	gate := make(chan struct{}, 1)
	library.Gate = gate
	convert := service.Execute
	service.Execute = func(ctx context.Context, source, stage, profile string) (string, error) {
		select {
		case gate <- struct{}{}:
			defer func() { <-gate }()
		case <-ctx.Done():
			return "", ctx.Err()
		}
		return convert(ctx, source, stage, profile)
	}
	service.LibraryShrink = library
	go library.Run(ctx)
	go service.Run(ctx)
	server := &http.Server{Addr: "127.0.0.1:8789", Handler: service.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, IdleTimeout: time.Minute}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		server.Shutdown(shutdown)
	}()
	if err = server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
