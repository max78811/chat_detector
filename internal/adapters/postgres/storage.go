package postgres

import (
	"context"
	"fmt"
	"time"

	"tg_bot/internal/domain"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Storage struct {
	pool *pgxpool.Pool
}

func New(ctx context.Context, databaseURL string) (*Storage, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return &Storage{pool: pool}, nil
}

func (s *Storage) Close() error {
	s.pool.Close()
	return nil
}

// --- UserStorage ---

func (s *Storage) SaveUser(ctx context.Context, user *domain.User) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO users (id, first_name, last_name, username, is_premium)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (id) DO UPDATE SET
			first_name = EXCLUDED.first_name,
			last_name  = EXCLUDED.last_name,
			username   = EXCLUDED.username,
			is_premium = EXCLUDED.is_premium,
			updated_at = NOW()
	`, user.ID, user.FirstName, user.LastName, user.Username, user.IsPremium)
	return err
}

func (s *Storage) GetUser(ctx context.Context, id int64) (*domain.User, error) {
	var user domain.User
	err := s.pool.QueryRow(ctx, `
		SELECT id, first_name, last_name, username, is_premium
		FROM users WHERE id = $1
	`, id).Scan(&user.ID, &user.FirstName, &user.LastName, &user.Username, &user.IsPremium)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return &user, err
}

// --- SubscriberStorage ---

func (s *Storage) AddSubscriber(ctx context.Context, sub *domain.Subscriber) error {
	// Ensure the user exists first.
	_, err := s.pool.Exec(ctx, `
		INSERT INTO users (id) VALUES ($1) ON CONFLICT DO NOTHING
	`, sub.UserID)
	if err != nil {
		return err
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO subscribers (user_id, bot_chat_id, created_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id) DO UPDATE SET bot_chat_id = EXCLUDED.bot_chat_id
	`, sub.UserID, sub.BotChatID, time.Now())
	return err
}

func (s *Storage) RemoveSubscriber(ctx context.Context, userID int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM subscribers WHERE user_id = $1`, userID)
	return err
}

func (s *Storage) GetSubscriber(ctx context.Context, userID int64) (*domain.Subscriber, error) {
	var sub domain.Subscriber
	err := s.pool.QueryRow(ctx, `
		SELECT user_id, bot_chat_id, created_at
		FROM subscribers WHERE user_id = $1
	`, userID).Scan(&sub.UserID, &sub.BotChatID, &sub.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return &sub, err
}

// --- BusinessConnectionStorage ---

func (s *Storage) SaveBusinessConnection(ctx context.Context, conn *domain.BusinessConnection) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO business_connections (id, user_id, chat_id, is_enabled, created_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (id) DO UPDATE SET
			is_enabled = EXCLUDED.is_enabled
	`, conn.ID, conn.UserID, conn.ChatID, conn.IsEnabled, conn.CreatedAt)
	return err
}

func (s *Storage) GetBusinessConnection(ctx context.Context, id string) (*domain.BusinessConnection, error) {
	var conn domain.BusinessConnection
	err := s.pool.QueryRow(ctx, `
		SELECT id, user_id, chat_id, is_enabled, created_at
		FROM business_connections WHERE id = $1
	`, id).Scan(&conn.ID, &conn.UserID, &conn.ChatID, &conn.IsEnabled, &conn.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return &conn, err
}

func (s *Storage) GetBusinessConnectionByUser(ctx context.Context, userID int64) (*domain.BusinessConnection, error) {
	var conn domain.BusinessConnection
	err := s.pool.QueryRow(ctx, `
		SELECT id, user_id, chat_id, is_enabled, created_at
		FROM business_connections WHERE user_id = $1 AND is_enabled = true
		ORDER BY created_at DESC LIMIT 1
	`, userID).Scan(&conn.ID, &conn.UserID, &conn.ChatID, &conn.IsEnabled, &conn.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return &conn, err
}
