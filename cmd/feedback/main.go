package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"example.com/ratingdon/internal/config"
	"example.com/ratingdon/internal/feedback"
	"example.com/ratingdon/internal/httpapi"
	"example.com/ratingdon/internal/postgres"
	"example.com/ratingdon/internal/slideshow"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, logger); err != nil {
		logger.Error("application stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	player, err := slideshow.New(cfg.SlideshowURL)
	if err != nil {
		return fmt.Errorf("SLIDESHOW_URL: %w", err)
	}
	startupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	pool, err := postgres.Connect(startupCtx, cfg.DatabaseURL)
	cancel()
	if err != nil {
		// A database error may contain connection credentials. Keep them out of logs.
		return errors.New("database connection or startup check failed")
	}
	defer pool.Close()
	devices := make([]httpapi.Device, len(cfg.Devices))
	for i, device := range cfg.Devices {
		devices[i] = httpapi.Device{ID: device.ID, Name: device.Name}
	}
	router := httpapi.NewRouterWithOptions(feedback.NewStore(pool), httpapi.Options{
		Devices: devices, RequestTTL: cfg.RequestTTL,
		AdminToken: cfg.AdminToken, DeviceTokens: cfg.DeviceTokens,
		Slideshow: player, Ready: pool.Ping,
	})
	server := &http.Server{
		Addr: cfg.Addr, Handler: router,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	serveError := make(chan error, 1)
	go func() { serveError <- server.Serve(listener) }()
	logger.Info("application started", "address", listener.Addr().String(),
		"devices", len(devices), "request_ttl", cfg.RequestTTL.String(),
		"admin_auth", cfg.AdminToken != "", "slideshow", player != nil)
	select {
	case err := <-serveError:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("HTTP server: %w", err)
		}
		return nil
	case <-ctx.Done():
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return fmt.Errorf("HTTP shutdown: %w", err)
		}
		if err := <-serveError; !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("HTTP server: %w", err)
		}
		logger.Info("application stopped")
		return nil
	}
}
