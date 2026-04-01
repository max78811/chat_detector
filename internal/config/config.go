package config

import (
	"fmt"
	"os"
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

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}
