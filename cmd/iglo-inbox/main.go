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
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	service, err := inbox.New("/var/lib/iglo-inbox/worker", "/data/inbox", "/data/videos/home-videos")
	if err != nil {
		log.Fatal(err)
	}
	service.AdmissionDir = "/var/lib/iglo-inbox/admission"
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
