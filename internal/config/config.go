package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	// Bot token from @BotFather
	BotToken string

	// PostgreSQL
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string

	// Paths
	MediaDir string

	// Telegram user IDs allowed to use admin commands
	AdminIDs []int64
}

func (c *Config) DatabaseURL() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		c.DBUser, c.DBPassword, c.DBHost, c.DBPort, c.DBName)
}

func Load() (*Config, error) {
	cfg := &Config{
		BotToken:   getEnv("TG_BOT_TOKEN", ""),
		DBHost:     getEnv("DB_HOST", "localhost"),
		DBPort:     getEnv("DB_PORT", "5432"),
		DBUser:     getEnv("DB_USER", "tgbot"),
		DBPassword: getEnv("DB_PASSWORD", "tgbot"),
		DBName:     getEnv("DB_NAME", "tgbot"),
		MediaDir:   getEnv("MEDIA_DIR", "./media"),
	}

	if cfg.BotToken == "" {
		return nil, fmt.Errorf("TG_BOT_TOKEN is required")
	}

	for _, raw := range strings.Split(getEnv("ADMIN_IDS", ""), ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid ADMIN_IDS value %q: %w", raw, err)
		}
		cfg.AdminIDs = append(cfg.AdminIDs, id)
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}
