package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
	_ "time/tzdata" // schedules are defined in the user's time zone; the container has no zone database

	"freecenter/internal/api"
	"freecenter/internal/esxi"
	"freecenter/internal/store"
	"freecenter/web"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	dataDir := env("FREECENTER_DATA", "./data")
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		logger.Error("create data dir", "err", err)
		os.Exit(1)
	}

	if len(os.Args) > 1 {
		if os.Args[1] != "reset-password" || len(os.Args) > 3 {
			fmt.Fprintln(os.Stderr, "usage: freecenter [reset-password [username]]")
			os.Exit(2)
		}
		username := env("FREECENTER_ADMIN_USER", "admin")
		if len(os.Args) == 3 {
			username = os.Args[2]
		}
		if err := resetPassword(filepath.Join(dataDir, "store.json"), username); err != nil {
			fmt.Fprintln(os.Stderr, "reset-password:", err)
			os.Exit(1)
		}
		return
	}

	st, err := store.Open(filepath.Join(dataDir, "store.json"))
	if err != nil {
		logger.Error("open store", "err", err)
		os.Exit(1)
	}

	// bootstrap admin user
	adminUser := env("FREECENTER_ADMIN_USER", "admin")
	adminPass := env("FREECENTER_ADMIN_PASSWORD", "admin")
	if created, err := st.EnsureAdmin(adminUser, adminPass); err != nil {
		logger.Error("bootstrap admin", "err", err)
		os.Exit(1)
	} else if created {
		logger.Warn("bootstrap admin user created", "username", adminUser, "hint", "password from FREECENTER_ADMIN_PASSWORD (default: admin); change it after the first login")
	}

	mgr := esxi.NewManager(st.Host, filepath.Join(dataDir, "known_hosts"))
	srv := api.NewServer(st, mgr, logger, filepath.Join(dataDir, "known_hosts"))

	mux := http.NewServeMux()
	routes := srv.Routes()
	mux.Handle("/api/", routes)
	mux.Handle("/auth/", routes)
	mux.Handle("/", web.Handler())

	addr := env("FREECENTER_ADDR", ":8080")
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 30 * time.Second,
	}

	go func() {
		logger.Info("freecenter listening", "addr", addr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	logger.Info("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(ctx)
}
