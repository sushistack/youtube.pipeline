package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/sushistack/youtube.pipeline/internal/api"
	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/config"
	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
	"github.com/sushistack/youtube.pipeline/internal/service"
	"github.com/sushistack/youtube.pipeline/internal/web"

	_ "github.com/ncruces/go-sqlite3/driver"
)

func newServeCmd() *cobra.Command {
	var (
		port    int
		devMode bool
	)

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the HTTP API server (localhost only)",
		Long: `Start the youtube.pipeline HTTP server bound to 127.0.0.1.

Use --dev to proxy non-/api/ requests to the Vite dev server (default: localhost:5173).
Without --dev, the embedded SPA is served directly.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServe(cmd, port, devMode)
		},
	}

	cmd.Flags().IntVar(&port, "port", 8080, "port to listen on (bound to 127.0.0.1 only)")
	cmd.Flags().BoolVar(&devMode, "dev", false, "proxy frontend requests to Vite dev server")
	return cmd
}

func runServe(cmd *cobra.Command, port int, devMode bool) error {
	cfg, err := config.Load(cfgPath, config.DefaultEnvPath())
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	database, err := db.OpenDB(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()

	store := db.NewRunStore(database)
	segStore := db.NewSegmentStore(database)

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	engine := pipeline.NewEngine(store, segStore, clock.RealClock{}, cfg.OutputDir, logger)
	svc := service.NewRunService(store, engine)

	mux := http.NewServeMux()

	deps := api.NewDependencies(svc, cfg.OutputDir, logger, web.FS)
	if devMode {
		// In dev mode replace the SPA catch-all with a Vite reverse proxy.
		deps.WebFS = nil
		api.RegisterRoutes(mux, deps)

		viteURL, _ := url.Parse("http://localhost:5173")
		proxy := httputil.NewSingleHostReverseProxy(viteURL)
		mux.Handle("/", proxy)
		fmt.Fprintf(cmd.OutOrStdout(), "Dev mode: proxying frontend to http://localhost:5173\n")
	} else {
		api.RegisterRoutes(mux, deps)
	}

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Listening on http://%s\n", addr)

	// Graceful shutdown on SIGINT / SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Separate channels for server-start failure vs. clean exit so that a
	// graceful ErrServerClosed never surfaces as a bogus "server error: <nil>".
	errCh := make(chan error, 1)
	go func() {
		err := srv.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			return
		}
		errCh <- err
	}()

	select {
	case err := <-errCh:
		if err == nil {
			return nil
		}
		return fmt.Errorf("server error: %w", err)
	case <-ctx.Done():
		fmt.Fprintln(cmd.OutOrStdout(), "\nShutting down...")
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutCtx)
	}
}
