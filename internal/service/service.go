package service

import (
	"context"

	"tg_bot/internal/adapters/telegram"
	"tg_bot/internal/config"
	"tg_bot/internal/ports"

	"go.uber.org/zap"
)

// Service orchestrates the bot.
type Service struct {
	bot     *telegram.Bot
	storage ports.Storage
	log     *zap.Logger
}

func New(cfg *config.Config, storage ports.Storage, log *zap.Logger) (*Service, error) {
	bot, err := telegram.NewBot(cfg.BotToken, storage, cfg.MediaDir, log.Named("bot"))
	if err != nil {
		return nil, err
	}

	return &Service{
		bot:     bot,
		storage: storage,
		log:     log,
	}, nil
}

// Run starts the bot polling loop.
func (s *Service) Run(ctx context.Context) error {
	s.log.Info("starting bot with business message support")
	return s.bot.Run(ctx)
}

func (s *Service) Shutdown() {
	if err := s.storage.Close(); err != nil {
		s.log.Error("close storage", zap.Error(err))
	}
}
