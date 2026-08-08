package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Geno1024-AIGC/port-forwarder/internal/api"
	"github.com/Geno1024-AIGC/port-forwarder/internal/engine"
	"github.com/Geno1024-AIGC/port-forwarder/internal/webui"
)

const version = "0.2.0"

func main() {
	var webAddr string
	flag.StringVar(&webAddr, "web", ":7070", "address for the embedded web admin UI")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "port-forwarder %s\n\n", version)
		fmt.Fprintf(flag.CommandLine.Output(), "A TCP port-forwarding daemon with an embedded web admin UI.\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	eng := engine.New()
	eng.SetErrorHandler(func(id string, err error) {
		slog.Error("rule error", "id", id, "err", err)
	})

	apiServer := api.New(eng)
	mux := http.NewServeMux()
	mux.Handle("/api/", apiServer.Handler())
	mux.Handle("/ui/", http.StripPrefix("/ui/", webui.Handler()))
	mux.Handle("/", http.RedirectHandler("/ui/", http.StatusFound))

	srv := &http.Server{Addr: webAddr, Handler: mux}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		slog.Info("web UI listening", "addr", webAddr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		slog.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}
}
