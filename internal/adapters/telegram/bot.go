package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"tg_bot/internal/domain"
	"tg_bot/internal/ports"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.uber.org/zap"
)

const (
	// retention is how long cached messages and downloaded media are kept.
	retention       = 7 * 24 * time.Hour
	cleanupInterval = time.Hour
)

// cachedMessage holds a message in memory for edit/delete detection.
type cachedMessage struct {
	SenderID   int64
	SenderName string
	Text       string
	MediaPath  string
	MediaType  string
	Date       time.Time
	ChatID     int64
	CachedAt   time.Time
}

// msgKey uniquely identifies a message within a chat.
type msgKey struct {
	MessageID int
	ChatID    int64
}

// Bot handles Telegram Business Bot API updates and implements the Notifier port.
type Bot struct {
	api      *tgbotapi.BotAPI
	token    string
	storage  ports.Storage
	mediaDir string
	admins   map[int64]bool
	log      *zap.Logger

	mu    sync.RWMutex
	cache map[msgKey]*cachedMessage
}

func NewBot(token string, storage ports.Storage, mediaDir string, adminIDs []int64, log *zap.Logger) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("create bot api: %w", err)
	}

	log.Info("authorized bot", zap.String("username", api.Self.UserName))

	admins := make(map[int64]bool, len(adminIDs))
	for _, id := range adminIDs {
		admins[id] = true
	}

	return &Bot{
		api:      api,
		token:    token,
		storage:  storage,
		mediaDir: mediaDir,
		admins:   admins,
		log:      log,
		cache:    make(map[msgKey]*cachedMessage),
	}, nil
}

// --- Raw Telegram Bot API types for Business features ---

type RawUpdate struct {
	UpdateID                int                    `json:"update_id"`
	Message                 *RawMessage            `json:"message"`
	BusinessConnection      *RawBusinessConnection `json:"business_connection"`
	BusinessMessage         *RawMessage            `json:"business_message"`
	EditedBusinessMessage   *RawMessage            `json:"edited_business_message"`
	DeletedBusinessMessages *RawDeletedBizMessages `json:"deleted_business_messages"`
}

type RawBusinessConnection struct {
	ID         string  `json:"id"`
	User       RawUser `json:"user"`
	UserChatID int64   `json:"user_chat_id"`
	Date       int64   `json:"date"`
	IsEnabled  bool    `json:"is_enabled"`
}

type RawDeletedBizMessages struct {
	BusinessConnectionID string  `json:"business_connection_id"`
	Chat                 RawChat `json:"chat"`
	Date                 int64   `json:"date"`
	MessageIDs           []int   `json:"message_ids"`
}

type RawMessage struct {
	MessageID            int           `json:"message_id"`
	From                 *RawUser      `json:"from"`
	Chat                 RawChat       `json:"chat"`
	Date                 int64         `json:"date"`
	Text                 string        `json:"text"`
	BusinessConnectionID string        `json:"business_connection_id"`
	Photo                []RawPhoto    `json:"photo"`
	Video                *RawVideo     `json:"video"`
	Document             *RawDoc       `json:"document"`
	VideoNote            *RawVideoNote `json:"video_note"`
	Voice                *RawVoice     `json:"voice"`
}

type RawUser struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
	IsPremium bool   `json:"is_premium"`
	IsBot     bool   `json:"is_bot"`
}

type RawChat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

type RawPhoto struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	FileSize     int    `json:"file_size"`
}

type RawVideo struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	Duration     int    `json:"duration"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	FileName     string `json:"file_name"`
	FileSize     int    `json:"file_size"`
}

type RawDoc struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	FileName     string `json:"file_name"`
	FileSize     int    `json:"file_size"`
}

type RawVideoNote struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	Length       int    `json:"length"`
	Duration     int    `json:"duration"`
	FileSize     int    `json:"file_size"`
}

type RawVoice struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	Duration     int    `json:"duration"`
	MimeType     string `json:"mime_type"`
	FileSize     int    `json:"file_size"`
}

type getUpdatesResponse struct {
	OK     bool        `json:"ok"`
	Result []RawUpdate `json:"result"`
}

// Run starts the long-polling loop.
func (b *Bot) Run(ctx context.Context) error {
	if err := os.MkdirAll(b.mediaDir, 0o755); err != nil {
		return fmt.Errorf("create media dir: %w", err)
	}

	go b.cleanupLoop(ctx)

	offset := 0
	client := &http.Client{Timeout: 65 * time.Second}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		url := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates?offset=%d&timeout=60&allowed_updates=%s",
			b.token, offset, `["message","business_connection","business_message","edited_business_message","deleted_business_messages"]`)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}

		resp, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			b.log.Error("get updates", zap.Error(err))
			time.Sleep(3 * time.Second)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			b.log.Error("read response", zap.Error(err))
			continue
		}

		var result getUpdatesResponse
		if err := json.Unmarshal(body, &result); err != nil {
			b.log.Error("unmarshal updates", zap.Error(err))
			continue
		}

		if !result.OK {
			b.log.Error("telegram api error", zap.String("body", string(body)))
			time.Sleep(3 * time.Second)
			continue
		}

		for _, upd := range result.Result {
			offset = upd.UpdateID + 1
			b.handleRawUpdate(ctx, &upd)
		}
	}
}

func (b *Bot) handleRawUpdate(ctx context.Context, upd *RawUpdate) {
	switch {
	case upd.Message != nil && upd.Message.BusinessConnectionID == "":
		b.handleCommand(ctx, upd.Message)
	case upd.BusinessConnection != nil:
		b.handleBusinessConnection(ctx, upd.BusinessConnection)
	case upd.BusinessMessage != nil:
		b.handleBusinessMessage(ctx, upd.BusinessMessage)
	case upd.EditedBusinessMessage != nil:
		b.handleEditedBusinessMessage(ctx, upd.EditedBusinessMessage)
	case upd.DeletedBusinessMessages != nil:
		b.handleDeletedBusinessMessages(ctx, upd.DeletedBusinessMessages)
	}
}

// --- Command handling ---

func (b *Bot) handleCommand(ctx context.Context, msg *RawMessage) {
	if msg.Text == "" || msg.Text[0] != '/' {
		return
	}

	cmd := msg.Text
	for i, c := range cmd {
		if c == ' ' || c == '@' {
			cmd = cmd[:i]
			break
		}
	}

	switch cmd {
	case "/start":
		b.handleStart(ctx, msg)
	case "/stop":
		b.handleStop(ctx, msg)
	case "/status":
		b.handleStatus(ctx, msg)
	case "/users":
		b.handleUsers(ctx, msg)
	default:
		b.sendText(msg.Chat.ID, "Неизвестная команда. Используйте /start, /stop или /status.")
	}
}

func (b *Bot) handleStart(ctx context.Context, msg *RawMessage) {
	user := msg.From
	if user == nil {
		return
	}

	if !user.IsPremium {
		b.sendText(msg.Chat.ID,
			"⛔ Этот бот доступен только для пользователей Telegram Premium.\n"+
				"Подключите Premium и попробуйте снова.")
		return
	}

	if err := b.storage.SaveUser(ctx, &domain.User{
		ID:        user.ID,
		FirstName: user.FirstName,
		LastName:  user.LastName,
		Username:  user.Username,
		IsPremium: user.IsPremium,
	}); err != nil {
		b.log.Error("save user", zap.Error(err))
	}

	if err := b.storage.AddSubscriber(ctx, &domain.Subscriber{
		UserID:    user.ID,
		BotChatID: msg.Chat.ID,
		CreatedAt: time.Now(),
	}); err != nil {
		b.log.Error("add subscriber", zap.Error(err))
		b.sendText(msg.Chat.ID, "Произошла ошибка. Попробуйте позже.")
		return
	}

	b.sendText(msg.Chat.ID,
		"✅ Вы подписаны на уведомления!\n\n"+
			"Теперь подключите бота через настройки Telegram:\n"+
			"<b>Настройки → Telegram Business → Чат-боты</b>\n\n"+
			"Бот будет сохранять удалённые и изменённые сообщения из ваших чатов, "+
			"включая фото и видео.\n\n"+
			"Используйте /stop для отключения.")
	b.log.Info("user subscribed",
		zap.Int64("user_id", user.ID),
		zap.String("username", user.Username))
}

func (b *Bot) handleStop(ctx context.Context, msg *RawMessage) {
	if msg.From == nil {
		return
	}

	if err := b.storage.RemoveSubscriber(ctx, msg.From.ID); err != nil {
		b.log.Error("remove subscriber", zap.Error(err))
		b.sendText(msg.Chat.ID, "Произошла ошибка. Попробуйте позже.")
		return
	}

	b.sendText(msg.Chat.ID, "🔕 Вы отписаны от уведомлений. Используйте /start чтобы подписаться снова.")
	b.log.Info("user unsubscribed", zap.Int64("user_id", msg.From.ID))
}

func (b *Bot) handleStatus(ctx context.Context, msg *RawMessage) {
	if msg.From == nil {
		return
	}

	sub, err := b.storage.GetSubscriber(ctx, msg.From.ID)
	if err != nil {
		b.log.Error("get subscriber", zap.Error(err))
		b.sendText(msg.Chat.ID, "Произошла ошибка.")
		return
	}

	if sub == nil {
		b.sendText(msg.Chat.ID, "❌ Вы не подписаны. Используйте /start.")
		return
	}

	conn, err := b.storage.GetBusinessConnectionByUser(ctx, msg.From.ID)
	if err != nil {
		b.log.Error("get business connection", zap.Error(err))
	}

	status := fmt.Sprintf("✅ Вы подписаны с %s", sub.CreatedAt.Format("02.01.2006 15:04"))
	if conn != nil && conn.IsEnabled {
		status += "\n🔗 Бизнес-подключение активно."
	} else {
		status += "\n⚠️ Бизнес-подключение не активно. Подключите бота через настройки Telegram Business."
	}

	b.sendText(msg.Chat.ID, status)
}

// --- Admin commands ---

func (b *Bot) isAdmin(msg *RawMessage) bool {
	return msg.From != nil && b.admins[msg.From.ID]
}

// maxMessageLen is kept below Telegram's 4096-char limit to leave room for markup.
const maxMessageLen = 4000

func (b *Bot) handleUsers(ctx context.Context, msg *RawMessage) {
	if !b.isAdmin(msg) {
		b.sendText(msg.Chat.ID, "Неизвестная команда. Используйте /start, /stop или /status.")
		return
	}

	users, err := b.storage.ListUsers(ctx)
	if err != nil {
		b.log.Error("list users", zap.Error(err))
		b.sendText(msg.Chat.ID, "Произошла ошибка.")
		return
	}

	if len(users) == 0 {
		b.sendText(msg.Chat.ID, "Пользователей пока нет.")
		return
	}

	subscribed, connected := 0, 0
	lines := make([]string, 0, len(users))
	for i, u := range users {
		if u.IsSubscribed {
			subscribed++
		}
		if u.HasConnection {
			connected++
		}

		flags := ""
		if u.IsPremium {
			flags += "⭐"
		}
		if u.IsSubscribed {
			flags += "🔔"
		}
		if u.HasConnection {
			flags += "🔗"
		}

		lines = append(lines, fmt.Sprintf("%d. %s <code>%d</code> %s\n   📅 %s",
			i+1,
			html.EscapeString(formatUserName(u.FirstName, u.LastName, u.Username)),
			u.ID,
			flags,
			u.RegisteredAt.Format("02.01.2006 15:04")))
	}

	header := fmt.Sprintf("👥 <b>Пользователи: %d</b>\n🔔 Подписаны: %d\n🔗 Бизнес-подключение: %d\n",
		len(users), subscribed, connected)

	// Split into several messages to stay within Telegram's length limit.
	var sb strings.Builder
	sb.WriteString(header)
	for _, line := range lines {
		if sb.Len()+len(line)+1 > maxMessageLen {
			b.sendText(msg.Chat.ID, sb.String())
			sb.Reset()
		}
		sb.WriteString("\n")
		sb.WriteString(line)
	}
	b.sendText(msg.Chat.ID, sb.String())
}

// --- Business connection handling ---

func (b *Bot) handleBusinessConnection(ctx context.Context, conn *RawBusinessConnection) {
	b.log.Info("business connection update",
		zap.String("id", conn.ID),
		zap.Int64("user_id", conn.User.ID),
		zap.Bool("enabled", conn.IsEnabled))

	if err := b.storage.SaveUser(ctx, &domain.User{
		ID:        conn.User.ID,
		FirstName: conn.User.FirstName,
		LastName:  conn.User.LastName,
		Username:  conn.User.Username,
		IsPremium: conn.User.IsPremium,
	}); err != nil {
		b.log.Error("save user from business connection", zap.Error(err))
	}

	if err := b.storage.SaveBusinessConnection(ctx, &domain.BusinessConnection{
		ID:        conn.ID,
		UserID:    conn.User.ID,
		ChatID:    conn.UserChatID,
		IsEnabled: conn.IsEnabled,
		CreatedAt: time.Unix(conn.Date, 0),
	}); err != nil {
		b.log.Error("save business connection", zap.Error(err))
		return
	}

	sub, _ := b.storage.GetSubscriber(ctx, conn.User.ID)
	if sub != nil {
		if conn.IsEnabled {
			b.sendText(sub.BotChatID, "🔗 Бизнес-подключение активировано! Бот теперь отслеживает ваши чаты.")
		} else {
			b.sendText(sub.BotChatID, "🔕 Бизнес-подключение отключено. Бот больше не отслеживает ваши чаты.")
		}
	}
}

// --- Business message handling (in-memory cache, no DB) ---

func (b *Bot) handleBusinessMessage(ctx context.Context, msg *RawMessage) {
	if msg.From != nil && msg.From.IsBot {
		return
	}

	senderName := "Unknown"
	senderID := int64(0)
	if msg.From != nil {
		senderID = msg.From.ID
		senderName = formatUserName(msg.From.FirstName, msg.From.LastName, msg.From.Username)
		b.saveRawUser(ctx, msg.From)
	}

	// Download media if present.
	mediaPath, mediaType := b.downloadMedia(ctx, msg)

	// Cache the message in memory.
	key := msgKey{MessageID: msg.MessageID, ChatID: msg.Chat.ID}
	b.mu.Lock()
	b.cache[key] = &cachedMessage{
		SenderID:   senderID,
		SenderName: senderName,
		Text:       msg.Text,
		MediaPath:  mediaPath,
		MediaType:  mediaType,
		Date:       time.Unix(msg.Date, 0),
		ChatID:     msg.Chat.ID,
		CachedAt:   time.Now(),
	}
	b.mu.Unlock()

	b.log.Debug("cached business message",
		zap.Int("msg_id", msg.MessageID),
		zap.Int64("chat_id", msg.Chat.ID))
}

func (b *Bot) handleEditedBusinessMessage(ctx context.Context, msg *RawMessage) {
	key := msgKey{MessageID: msg.MessageID, ChatID: msg.Chat.ID}

	b.mu.RLock()
	original, exists := b.cache[key]
	b.mu.RUnlock()

	if !exists {
		// Don't have the original — just cache the current version.
		b.handleBusinessMessage(ctx, msg)
		return
	}

	// Find the owner of this business connection.
	conn, err := b.storage.GetBusinessConnection(ctx, msg.BusinessConnectionID)
	if err != nil || conn == nil {
		b.log.Error("get business connection for edit notification", zap.Error(err))
		return
	}

	sub, err := b.storage.GetSubscriber(ctx, conn.UserID)
	if err != nil || sub == nil {
		return
	}

	// Notify about edit.
	b.notifyEdited(sub.BotChatID, original, msg.Text)

	// Update cache with new text.
	b.mu.Lock()
	original.Text = msg.Text
	b.mu.Unlock()
}

func (b *Bot) handleDeletedBusinessMessages(ctx context.Context, del *RawDeletedBizMessages) {
	// Find the owner of this business connection.
	conn, err := b.storage.GetBusinessConnection(ctx, del.BusinessConnectionID)
	if err != nil || conn == nil {
		b.log.Error("get business connection for delete notification", zap.Error(err))
		return
	}

	sub, err := b.storage.GetSubscriber(ctx, conn.UserID)
	if err != nil || sub == nil {
		return
	}

	for _, msgID := range del.MessageIDs {
		key := msgKey{MessageID: msgID, ChatID: del.Chat.ID}

		b.mu.Lock()
		cached, exists := b.cache[key]
		delete(b.cache, key)
		b.mu.Unlock()

		if !exists {
			// We don't have the message content — send a generic notification.
			b.sendText(sub.BotChatID, fmt.Sprintf("🗑 Удалено сообщение (ID: %d) из чата %d", msgID, del.Chat.ID))
			continue
		}

		b.notifyDeleted(ctx, sub.BotChatID, cached)
	}
}

// --- Notifications ---

func (b *Bot) notifyEdited(chatID int64, original *cachedMessage, newText string) {
	text := fmt.Sprintf(
		"✏️ <b>Изменённое сообщение</b>\n"+
			"👤 От: <b>%s</b>\n"+
			"📅 Дата: %s\n\n"+
			"📝 Было:\n<pre>%s</pre>\n\n"+
			"📝 Стало:\n<pre>%s</pre>",
		original.SenderName,
		original.Date.Format("02.01.2006 15:04"),
		original.Text,
		newText,
	)
	b.sendText(chatID, text)
}

func (b *Bot) notifyDeleted(ctx context.Context, chatID int64, msg *cachedMessage) {
	text := fmt.Sprintf(
		"🗑 <b>Удалённое сообщение</b>\n"+
			"👤 От: <b>%s</b>\n"+
			"📅 Дата: %s\n",
		msg.SenderName,
		msg.Date.Format("02.01.2006 15:04"),
	)

	if msg.Text != "" {
		text += fmt.Sprintf("\n💬 Текст:\n<pre>%s</pre>", msg.Text)
	}

	if msg.MediaPath != "" && msg.MediaType != "" {
		text += fmt.Sprintf("\n📎 Медиа: %s (сохранено)", msg.MediaType)
		b.sendText(chatID, text)
		b.sendMedia(ctx, chatID, msg.MediaPath, msg.MediaType,
			fmt.Sprintf("Удалённое медиа от %s", msg.SenderName))
		return
	}

	b.sendText(chatID, text)
}

// --- Retention ---

func (b *Bot) cleanupLoop(ctx context.Context) {
	b.cleanup()

	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.cleanup()
		}
	}
}

// cleanup removes cached messages and media files older than retention.
func (b *Bot) cleanup() {
	cutoff := time.Now().Add(-retention)

	b.mu.Lock()
	removed := 0
	for key, msg := range b.cache {
		if msg.CachedAt.Before(cutoff) {
			delete(b.cache, key)
			removed++
		}
	}
	b.mu.Unlock()

	// Media files are cleaned by mtime, so leftovers from previous runs are removed too.
	entries, err := os.ReadDir(b.mediaDir)
	if err != nil {
		b.log.Error("read media dir", zap.Error(err))
		return
	}

	removedFiles := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil || !info.ModTime().Before(cutoff) {
			continue
		}
		if err := os.Remove(filepath.Join(b.mediaDir, e.Name())); err != nil {
			b.log.Error("remove media file", zap.String("name", e.Name()), zap.Error(err))
			continue
		}
		removedFiles++
	}

	if removed > 0 || removedFiles > 0 {
		b.log.Info("retention cleanup",
			zap.Int("messages", removed),
			zap.Int("files", removedFiles))
	}
}

// --- Media download ---

func (b *Bot) downloadMedia(ctx context.Context, msg *RawMessage) (path string, mediaType string) {
	var fileID string

	switch {
	case len(msg.Photo) > 0:
		biggest := msg.Photo[0]
		for _, p := range msg.Photo[1:] {
			if p.FileSize > biggest.FileSize {
				biggest = p
			}
		}
		fileID = biggest.FileID
		path = filepath.Join(b.mediaDir, fmt.Sprintf("photo_%d_%d.jpg", msg.MessageID, time.Now().UnixNano()))
		mediaType = "photo"

	case msg.Video != nil:
		fileID = msg.Video.FileID
		ext := ".mp4"
		if msg.Video.FileName != "" {
			if e := filepath.Ext(msg.Video.FileName); e != "" {
				ext = e
			}
		}
		path = filepath.Join(b.mediaDir, fmt.Sprintf("video_%d_%d%s", msg.MessageID, time.Now().UnixNano(), ext))
		mediaType = "video"

	case msg.VideoNote != nil:
		fileID = msg.VideoNote.FileID
		path = filepath.Join(b.mediaDir, fmt.Sprintf("videonote_%d_%d.mp4", msg.MessageID, time.Now().UnixNano()))
		mediaType = "video_note"

	case msg.Voice != nil:
		fileID = msg.Voice.FileID
		path = filepath.Join(b.mediaDir, fmt.Sprintf("voice_%d_%d.ogg", msg.MessageID, time.Now().UnixNano()))
		mediaType = "voice"

	case msg.Document != nil:
		fileID = msg.Document.FileID
		ext := ".bin"
		if msg.Document.FileName != "" {
			if e := filepath.Ext(msg.Document.FileName); e != "" {
				ext = e
			}
		}
		path = filepath.Join(b.mediaDir, fmt.Sprintf("doc_%d_%d%s", msg.MessageID, time.Now().UnixNano(), ext))
		mediaType = "document"

	default:
		return "", ""
	}

	if err := b.downloadFile(ctx, fileID, path); err != nil {
		b.log.Error("download media", zap.String("file_id", fileID), zap.Error(err))
		return "", ""
	}

	return path, mediaType
}

func (b *Bot) downloadFile(ctx context.Context, fileID string, destPath string) error {
	fileConfig := tgbotapi.FileConfig{FileID: fileID}
	file, err := b.api.GetFile(fileConfig)
	if err != nil {
		return fmt.Errorf("get file info: %w", err)
	}

	fileURL := file.Link(b.token)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return fmt.Errorf("create download request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download file: status %d", resp.StatusCode)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}

// --- Helpers ---

func (b *Bot) saveRawUser(ctx context.Context, u *RawUser) {
	if err := b.storage.SaveUser(ctx, &domain.User{
		ID:        u.ID,
		FirstName: u.FirstName,
		LastName:  u.LastName,
		Username:  u.Username,
		IsPremium: u.IsPremium,
	}); err != nil {
		b.log.Error("save user", zap.Int64("id", u.ID), zap.Error(err))
	}
}

func (b *Bot) sendText(chatID int64, text string) {
	m := tgbotapi.NewMessage(chatID, text)
	m.ParseMode = "HTML"
	if _, err := b.api.Send(m); err != nil {
		b.log.Error("send message", zap.Int64("chat_id", chatID), zap.Error(err))
	}
}

func (b *Bot) sendMedia(ctx context.Context, chatID int64, mediaPath string, mediaType string, caption string) {
	file, err := os.Open(mediaPath)
	if err != nil {
		b.log.Error("open media file", zap.Error(err))
		return
	}
	defer file.Close()

	fileReader := tgbotapi.FileReader{
		Name:   filepath.Base(mediaPath),
		Reader: file,
	}

	var msg tgbotapi.Chattable
	switch mediaType {
	case "photo":
		photo := tgbotapi.NewPhoto(chatID, fileReader)
		photo.Caption = caption
		msg = photo
	case "video":
		video := tgbotapi.NewVideo(chatID, fileReader)
		video.Caption = caption
		msg = video
	case "video_note":
		// Video notes don't support captions.
		msg = tgbotapi.NewVideoNote(chatID, 0, fileReader)
	case "voice":
		voice := tgbotapi.NewVoice(chatID, fileReader)
		voice.Caption = caption
		msg = voice
	default:
		doc := tgbotapi.NewDocument(chatID, fileReader)
		doc.Caption = caption
		msg = doc
	}

	_, err = b.api.Send(msg)
	if err == nil {
		return
	}

	b.log.Error("send media",
		zap.String("type", mediaType),
		zap.String("path", mediaPath),
		zap.Error(err))

	// Voice/video notes can be blocked by the recipient's privacy settings
	// (VOICE_MESSAGES_FORBIDDEN) — fall back to sending the file as a document.
	if mediaType != "video_note" && mediaType != "voice" {
		return
	}

	// The library closes the reader after upload, so send by path (reopened on upload).
	doc := tgbotapi.NewDocument(chatID, tgbotapi.FilePath(mediaPath))
	doc.Caption = caption
	// Keep Telegram from turning the file back into a voice/video note.
	doc.DisableContentTypeDetection = true
	if _, err := b.api.Send(doc); err != nil {
		b.log.Error("send media as document", zap.Error(err))
	}
}

func formatUserName(first, last, username string) string {
	name := first
	if last != "" {
		name += " " + last
	}
	if username != "" {
		name += " (@" + username + ")"
	}
	if name == "" {
		return "Unknown"
	}
	return name
}
