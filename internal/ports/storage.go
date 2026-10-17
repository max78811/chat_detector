package ports

import (
	"context"
	"tg_bot/internal/domain"
)

// UserStorage defines the port for user persistence.
type UserStorage interface {
	SaveUser(ctx context.Context, user *domain.User) error
	GetUser(ctx context.Context, id int64) (*domain.User, error)
	ListUsers(ctx context.Context) ([]domain.UserInfo, error)
}

// SubscriberStorage defines the port for subscriber persistence.
type SubscriberStorage interface {
	AddSubscriber(ctx context.Context, sub *domain.Subscriber) error
	RemoveSubscriber(ctx context.Context, userID int64) error
	GetSubscriber(ctx context.Context, userID int64) (*domain.Subscriber, error)
}

// BusinessConnectionStorage defines the port for business connection persistence.
type BusinessConnectionStorage interface {
	SaveBusinessConnection(ctx context.Context, conn *domain.BusinessConnection) error
	GetBusinessConnection(ctx context.Context, id string) (*domain.BusinessConnection, error)
	GetBusinessConnectionByUser(ctx context.Context, userID int64) (*domain.BusinessConnection, error)
}

// Storage combines all storage ports.
type Storage interface {
	UserStorage
	SubscriberStorage
	BusinessConnectionStorage
	Close() error
}
