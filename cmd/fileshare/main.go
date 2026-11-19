package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/paleblue-2032/fileshare/internal/auth"
	"github.com/paleblue-2032/fileshare/internal/config"
	"github.com/paleblue-2032/fileshare/internal/store"
	"github.com/paleblue-2032/fileshare/internal/web"
	"golang.org/x/net/netutil"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "hash-password":
			if err := hashPassword(); err != nil {
				fmt.Fprintln(os.Stderr, "error:", err)
				os.Exit(1)
			}
			return
		case "version", "-version", "--version":
			fmt.Println("fileshare", version)
			return
		case "help", "-h", "--help":
			usage()
			return
		default:
			fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
			usage()
			os.Exit(2)
		}
	}
	if err := run(); err != nil {
		slog.Error("fileshare stopped", "error", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `usage: fileshare [command]

commands:
  hash-password   read a password from stdin and print its hash
  version         print the version
`)
}

func hashPassword() error {
	data, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	if err != nil {
		return err
	}
	password := strings.TrimRight(string(data), "\r\n")
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	fmt.Println(hash)
	return nil
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if !auth.ValidHash(cfg.PasswordHash) {
		return errors.New("FILESHARE_PASSWORD_HASH is not a valid pbkdf2-sha256 hash")
	}
	if cfg.DAVPasswordHash != "" && !auth.ValidHash(cfg.DAVPasswordHash) {
		return errors.New("FILESHARE_DAV_PASSWORD_HASH is not a valid pbkdf2-sha256 hash")
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	st, err := store.Open(cfg.DataDir, cfg.QuotaBytes, cfg.MinFreeBytes)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer func() { _ = st.Close() }()

	app, err := web.New(cfg, st, logger)
	if err != nil {
		return fmt.Errorf("create web server: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sweep := func() {
		res := st.Sweep(time.Now(), cfg.RetentionDays, cfg.TrashDays, 500)
		if res.FilesExpired+res.TrashPurged+res.SharesPruned+res.Errors > 0 {
			logger.Info("cleanup sweep",
				"expired", res.FilesExpired,
				"trash_purged", res.TrashPurged,
				"shares_pruned", res.SharesPruned,
				"errors", res.Errors,
			)
		}
	}
	go func() {
		sweep()
		ticker := time.NewTicker(cfg.SweepInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sweep()
			}
		}
	}()

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return err
	}
	// Cap concurrent connections so a slowloris or half-open flood cannot
	// exhaust the process's file descriptors or goroutines.
	listener = netutil.LimitListener(listener, 128)
	errCh := make(chan error, 1)
	go func() {
		if cfg.TLSCert != "" {
			errCh <- srv.ServeTLS(listener, cfg.TLSCert, cfg.TLSKey)
			return
		}
		errCh <- srv.Serve(listener)
	}()

	used, limit := st.QuotaSnapshot()
	logger.Info("fileshare started",
		"version", version,
		"addr", cfg.Addr,
		"data_dir", cfg.DataDir,
		"used_bytes", used,
		"quota_bytes", limit,
		"retention_days", cfg.RetentionDays,
		"trash_days", cfg.TrashDays,
		"webdav", cfg.WebDAV,
	)

	select {
	case <-ctx.Done():
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return nil
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}
