package main

import (
	"context"
	"flag"
	"github.com/MoeclubM/metafusion-mcp/internal/server"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	migrate := flag.Bool("migrate", false, "explicitly apply MCP schema migrations")
	flag.Parse()
	c, e := server.LoadConfig()
	if e != nil {
		slog.Error("invalid configuration", "error", e)
		os.Exit(1)
	}
	store, e := server.OpenStore(c.DatabaseURL)
	if e != nil {
		slog.Error("database configuration failed")
		os.Exit(1)
	}
	defer store.DB.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	if *migrate {
		e = store.Migrate(ctx)
	} else {
		e = store.Check(ctx)
	}
	cancel()
	if e != nil {
		slog.Error("MCP schema unavailable; run explicit migration")
		os.Exit(1)
	}
	if *migrate {
		slog.Info("MCP schema migration complete")
		return
	}
	s := server.New(c, store)
	h := &http.Server{Addr: c.Listen, Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 600 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	stop, release := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer release()
	go func() {
		<-stop.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = h.Shutdown(ctx)
	}()
	slog.Info("MetaFusion MCP listening", "address", c.Listen)
	if e = h.ListenAndServe(); e != nil && e != http.ErrServerClosed {
		slog.Error("HTTP server failed")
		os.Exit(1)
	}
}
