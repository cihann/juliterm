package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	neturl "net/url"
	"os"
	"os/signal"
	"syscall"

	qrterminal "github.com/mdp/qrterminal/v3"

	"github.com/cihann/juliterm/internal/config"
	"github.com/cihann/juliterm/internal/server"
	"github.com/cihann/juliterm/internal/tmux"
	"github.com/cihann/juliterm/internal/tunnel"
)

func main() {
	// Flags
	port := flag.Int("port", 8080, "HTTP server port")
	token := flag.String("token", "", "shared authentication token (required)")
	tmuxPath := flag.String("tmux-path", "tmux", "path to tmux binary")
	tlsCert := flag.String("tls-cert", "", "path to TLS certificate file")
	tlsKey := flag.String("tls-key", "", "path to TLS private key file")
	tunnelFlag := flag.Bool("tunnel", false, "expose Juli via Cloudflare Quick Tunnel (requires cloudflared)")
	configPath := flag.String("config", "", "path to config file (default: ~/.config/juli/config.yaml)")
	name := flag.String("name", "", "machine name for the page title, Home Screen name and icon (default: short hostname)")
	flag.Parse()

	if *token == "" {
		// Also check JULI_TOKEN env var
		*token = os.Getenv("JULI_TOKEN")
		if *token == "" {
			fmt.Fprintln(os.Stderr, "Error: --token flag or JULI_TOKEN env var is required")
			flag.Usage()
			os.Exit(1)
		}
	}

	// Context with signal handling for graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Initialize tmux manager
	mgr := tmux.NewManager(*tmuxPath)
	if err := mgr.Start(ctx); err != nil {
		log.Fatalf("Failed to start tmux manager: %v", err)
	}
	defer mgr.Stop()

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Printf("WARNING: failed to load config: %v (continuing with defaults)", err)
		cfg = &config.Config{}
	}
	if len(cfg.Splits) > 0 {
		log.Printf("config: loaded %d custom split preset(s)", len(cfg.Splits))
	}

	// Initialize HTTP server
	srv := server.New(*port, *token, mgr, webFileSystem(), cfg)
	if *name == "" {
		*name = server.DefaultName()
	}
	srv.SetName(*name)

	// Tunnel: start concurrently so the local server is not blocked.
	if *tunnelFlag {
		stopCh := make(chan func(), 1)
		go func() {
			url, stop, err := tunnel.Start(ctx, *port)
			stopCh <- stop
			if err != nil {
				log.Printf("WARNING: Tunnel failed: %v", err)
				log.Printf("Juli will continue serving locally on port %d", *port)
				return
			}
			log.Printf("Tunnel: %s", url)
			log.Println("WARNING: Juli is now accessible from the internet. Ensure your --token is a strong, unique secret.")
			// Allow WebSocket connections from the tunnel origin.
			if u, err := neturl.Parse(url); err == nil && u.Host != "" {
				srv.AllowOrigin(u.Host)
			}
			qrterminal.GenerateHalfBlock(url, qrterminal.L, os.Stdout)
		}()
		defer func() {
			if stop := <-stopCh; stop != nil {
				stop()
			}
		}()
	}

	// TLS configuration
	if *tlsCert != "" && *tlsKey != "" {
		log.Printf("Juli starting on https://0.0.0.0:%d (TLS)", *port)
		if err := srv.StartTLS(ctx, *tlsCert, *tlsKey); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	} else {
		log.Printf("Juli starting on http://0.0.0.0:%d", *port)
		if err := srv.Start(ctx); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	}
}
