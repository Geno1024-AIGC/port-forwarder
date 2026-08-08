package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Geno1024-AIGC/port-forwarder/internal/api"
	"github.com/Geno1024-AIGC/port-forwarder/internal/engine"
	"github.com/Geno1024-AIGC/port-forwarder/internal/tunnel"
	"github.com/Geno1024-AIGC/port-forwarder/internal/webui"
)

var version = "0.3.0"

func main() {
	if len(os.Args) < 2 {
		runLocal(os.Args[1:])
		return
	}
	switch os.Args[1] {
	case "server":
		runServer(os.Args[2:])
	case "client":
		runClient(os.Args[2:])
	case "local", "daemon":
		runLocal(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "port-forwarder %s\nusage:\n", version)
		fmt.Fprintln(os.Stderr, "  pf                  local daemon with web UI")
		fmt.Fprintln(os.Stderr, "  pf server [flags]   public endpoint")
		fmt.Fprintln(os.Stderr, "  pf client [flags]   tunnel into a server")
		os.Exit(2)
	}
}

// normalizeAddr defaults a bare port to a full listen address.
func normalizeAddr(addr string) string {
	if addr == "" {
		return ":7000"
	}
	_, _, err := net.SplitHostPort(addr)
	if err != nil {
		return ":" + addr
	}
	return addr
}

// --- server ----------------------------------------------------------------

func runServer(args []string) {
	fs := flag.NewFlagSet("server", flag.ExitOnError)
	var ctrl string
	var web string
	fs.StringVar(&ctrl, "ctrl", ":7000", "control port for tunnel clients")
	fs.StringVar(&web, "web", "", "optional web UI showing connected clients")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "port-forwarder server [flags]")
		fs.PrintDefaults()
	}
	fs.Parse(args)

	srv, err := tunnel.NewServer(normalizeAddr(ctrl))
	if err != nil {
		slog.Error("server start failed", "err", err)
		os.Exit(1)
	}
	go func() {
		slog.Info("tunnel server listening", "ctrl", srv.Addr())
		if err := srv.Serve(); err != nil {
			slog.Error("tunnel server stopped", "err", err)
			os.Exit(1)
		}
	}()

	if web != "" {
		serveStatusWeb(web, srv)
	} else {
		waitForSignal()
	}
}

// serveStatusWeb hosts a read-only page showing connected clients.
func serveStatusWeb(addr string, srv *tunnel.Server) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"clients": srv.Clients()})
	})
	mux.Handle("/ui/", http.StripPrefix("/ui/", webui.Handler()))
	mux.Handle("/", http.RedirectHandler("/ui/", http.StatusFound))
	slog.Info("status web listening", "addr", addr)
	runHTTPServer(addr, mux)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// --- client ----------------------------------------------------------------

func runClient(args []string) {
	fs := flag.NewFlagSet("client", flag.ExitOnError)
	var server string
	var web string
	fs.StringVar(&server, "server", "", "server address, e.g. vps.example.com:7000 (required)")
	fs.StringVar(&web, "web", ":28774", "address for the embedded web admin UI")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "port-forwarder client [flags]")
		fs.PrintDefaults()
	}
	fs.Parse(args)

	if server == "" {
		fmt.Fprintln(os.Stderr, "port-forwarder client: -server is required")
		os.Exit(2)
	}

	eng := engine.New()
	tc := tunnel.NewClient(normalizeAddr(server))
	adapter := &clientBackend{client: tc}
	eng.SetRemoteBackend(adapter)
	adapter.onStatus = func(id, listen string, err error) {
		eng.UpdateRemoteStatus(id, listen, err)
	}
	tc.SetOnRule(func(r tunnel.ClientRule, err error) {
		adapter.onStatus(r.ID, r.Listen, err)
	})
	eng.SetErrorHandler(func(id string, err error) {
		slog.Error("forward rule error", "id", id, "err", err)
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go tc.Run(ctx)

	serveAdminWeb(web, eng)
}

// clientBackend bridges engine.RemoteBackend and tunnel.Client.
type clientBackend struct {
	client   *tunnel.Client
	onStatus func(id, listen string, err error)
}

func (b *clientBackend) AddRemote(rule engine.Rule) {
	b.client.Add(tunnel.ClientRule{
		ID:     rule.ID,
		Name:   rule.Name,
		Listen: rule.Listen,
		Target: rule.Target,
	})
	if b.onStatus != nil {
		b.onStatus(rule.ID, "", nil)
	}
}

func (b *clientBackend) RemoveRemote(id string) {
	b.client.Remove(id)
}

// --- local daemon -----------------------------------------------------------

func runLocal(args []string) {
	fs := flag.NewFlagSet("local", flag.ExitOnError)
	var web string
	fs.StringVar(&web, "web", ":28774", "address for the embedded web admin UI")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "port-forwarder [flags]")
		fs.PrintDefaults()
	}
	fs.Parse(args)

	eng := engine.New()
	eng.SetErrorHandler(func(id string, err error) {
		slog.Error("forward rule error", "id", id, "err", err)
	})
	serveAdminWeb(web, eng)
}

// serveAdminWeb hosts the rule-management UI plus REST API until a signal.
func serveAdminWeb(addr string, eng *engine.Engine) {
	mux := http.NewServeMux()
	mux.Handle("/api/", api.New(eng).Handler())
	mux.Handle("/ui/", http.StripPrefix("/ui/", webui.Handler()))
	mux.Handle("/", http.RedirectHandler("/ui/", http.StatusFound))
	slog.Info("web UI listening", "addr", addr)
	runHTTPServer(addr, mux)
}

// waitForSignal blocks until an interrupt or termination signal arrives.
func waitForSignal() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	slog.Info("shutting down")
}

// runHTTPServer serves handler until an interrupt or termination signal.
func runHTTPServer(addr string, handler http.Handler) {
	srv := &http.Server{Addr: addr, Handler: handler}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case <-ctx.Done():
		slog.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			slog.Error("http server error", "err", err)
			os.Exit(1)
		}
	}
}
