package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"tg_bot/internal/adapters/postgres"
	"tg_bot/internal/config"
	"tg_bot/internal/service"

	"github.com/joho/godotenv"
	"go.uber.org/zap"
)

func main() {
	// Logger.
	log, _ := zap.NewProduction()
	defer log.Sync()

	// Load .env if exists.
	_ = godotenv.Load()

	// Config.
	cfg, err := config.Load()
	if err != nil {
		log.Fatal("load config", zap.Error(err))
	}

	// Context with graceful shutdown.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		log.Info("received signal, shutting down", zap.String("signal", sig.String()))
		cancel()
	}()

	// Storage.
	store, err := postgres.New(ctx, cfg.DatabaseURL())
	if err != nil {
		log.Fatal("init storage", zap.Error(err))
	}

	// Service.
	svc, err := service.New(cfg, store, log)
	if err != nil {
		log.Fatal("init service", zap.Error(err))
	}
	defer svc.Shutdown()

	// Run.
	log.Info("starting tg_bot service")
	if err := svc.Run(ctx); err != nil {
		log.Fatal("service error", zap.Error(err))
	}
}
