package domain

import "time"

// User represents a Telegram user.
type User struct {
	ID        int64
	FirstName string
	LastName  string
	Username  string
	IsPremium bool
}

// Subscriber is a premium user who subscribed to the bot notifications.
type Subscriber struct {
	UserID    int64
	BotChatID int64 // chat ID where the bot sends notifications
	CreatedAt time.Time
}

// BusinessConnection represents a Telegram Business connection
// established when a premium user connects the bot via business settings.
type BusinessConnection struct {
	ID        string // unique connection ID from Telegram
	UserID    int64  // the premium user who connected the bot
	ChatID    int64  // private chat ID with the user
	IsEnabled bool
	CreatedAt time.Time
}
