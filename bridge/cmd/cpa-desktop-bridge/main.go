package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cpa-desktop-bridge/internal/bridge"
)

func main() {
	cfg, err := bridge.LoadConfig()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	handler, err := bridge.NewServer(cfg)
	if err != nil {
		log.Fatalf("create bridge: %v", err)
	}

	server := &http.Server{
		Addr:              cfg.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      45 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		log.Printf("CPA Desktop Bridge listening on %s", cfg.Listen)
		if serveErr := server.ListenAndServe(); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			log.Fatalf("serve: %v", serveErr)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = server.Shutdown(ctx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}
