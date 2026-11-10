package bot

import (
	"chat-analyzer/internal/storage"
	"context"
	"fmt"
	"log"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Bot объединяет клиент Телеграма и нашу базу данных
type Bot struct {
	api     *tgbotapi.BotAPI
	storage *storage.Storage
}

// New создает и проверяет подключение к Telegram API
func New(token string, s *storage.Storage) (*Bot, error) {
	// Стучимся в Telegram с нашим токеном(под капотом вызывается метод getMe)
	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("new api: %w", err)
	}

	log.Printf("Authorized on account %s", api.Self.UserName)

	return &Bot{
		api:     api,
		storage: s,
	}, nil

}

// Start запускает бесконечный цикл чтения сообщений (Long Polling)
func (b *Bot) Start(ctx context.Context) {
	// Настраиваем Long Polling: 0 — читать с первого непрочитанного, тайм-аут ожидания — 60 сек
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	// GetUpdatesChan запускает фоновую горутину и возвращает Go-канал с входящими событиями
	updates := b.api.GetUpdatesChan(u)

	for {
		select {
		// Сценарий 1: Нажали Ctrl + C (контекст отменился)
		case <-ctx.Done():
			log.Println("Stopping Telegram Bot, waiting for updates to finish...")
			b.api.StopReceivingUpdates()
			return
		// Сценарий 2: Пришло новое событие из Telegram
		case update, ok := <-updates:
			if !ok {
				return // Канал закрылся
			}

			// Игнорируем всё, что не является обычным текстовым сообщением (лайки, вступления в чат и т.д.)
			if update.Message == nil || update.Message.Text == "" {
				continue
			}

			// Если написали команду /top — отдаем статистику, если /me – отдаем персональную статистику
			if update.Message.IsCommand() {
				switch update.Message.Command() {
				case "top":
					b.handleTopCommand(ctx, update.Message.Chat.ID)
				case "me":
					b.handleMeCommand(ctx, update.Message)
				case "stats":
					b.handleStatsCommand(ctx, update.Message.Chat.ID)
				case "start", "help":
					helpText := "👋 Привет! Я бот-анализатор чата.\n\nДоступные команды:\n/top — топ-5 активных участников\n/me — твоя личная статистика\n/stats — общая сводка по чату\n/help — список команд"
					reply := tgbotapi.NewMessage(update.Message.Chat.ID, helpText)
					if _, err := b.api.Send(reply); err != nil {
						log.Printf("sending message: %v", err)
					}
				}
				continue
			}

			// Иначе - сохраняем сообщение в PostgreSql
			b.handleMessage(ctx, update.Message)

		}
	}

}

func (b *Bot) handleMessage(ctx context.Context, msg *tgbotapi.Message) {
	username := msg.From.UserName
	if username == "" {
		username = msg.From.FirstName // Если у юзера нет @ ника, берем его имая
	}

	dbMsg := storage.Message{
		ChatID:   msg.Chat.ID,
		UserID:   msg.From.ID,
		Username: username,
		Text:     msg.Text,
	}

	if err := b.storage.SaveMessage(ctx, dbMsg); err != nil {
		log.Printf("save message: %v", err)
	}
}

func (b *Bot) handleTopCommand(ctx context.Context, chatID int64) {
	stats, err := b.storage.GetTopUsers(ctx, chatID, 5)
	if err != nil {
		log.Printf("get top users: %v", err)
		return
	}

	if len(stats) == 0 {
		reply := tgbotapi.NewMessage(chatID, "Пока нет сохраненных сообщений для статистики")
		if _, err := b.api.Send(reply); err != nil {
			log.Printf("send message: %v", err)
		}
		return
	}

	// Собираем красивый текст ответа через strings Builder
	var sb strings.Builder
	sb.WriteString("📊 Топ-5 активных участников чата:\n\n")
	for i, stat := range stats {
		fmt.Fprintf(&sb, "%d. %s — %d сообщ.\n", i+1, stat.Username, stat.MessageCount)
	}
	reply := tgbotapi.NewMessage(chatID, sb.String())
	if _, err := b.api.Send(reply); err != nil {
		log.Printf("send message: %v", err)
	}
}

func (b *Bot) handleMeCommand(ctx context.Context, msg *tgbotapi.Message) {
	meStat, err := b.storage.GetUserStats(ctx, msg.Chat.ID, msg.From.ID)
	if err != nil {
		log.Printf("get user stats: %v", err)
		return
	}

	if meStat.MessageCount == 0 {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "Ты еще не написал ни одного сообщения в этом чате!")
		if _, err := b.api.Send(reply); err != nil {
			log.Printf("Error sending empty me reply: %v", err)
		}
		return
	}

	text := fmt.Sprintf("Твоя статистика:\nСообщений: %d\nПервое сообщение: %s",
		meStat.MessageCount,
		meStat.FirstSeen.Format("02.01.2006 15:04"),
	)
	reply := tgbotapi.NewMessage(msg.Chat.ID, text)
	if _, err := b.api.Send(reply); err != nil {
		log.Printf("send message: %v", err)
	}
}

func (b *Bot) handleStatsCommand(ctx context.Context, chatID int64) {
	stats, err := b.storage.GetChatStats(ctx, chatID)
	if err != nil {
		log.Printf("get stats: %v", err)
		return
	}

	if stats.TotalMessages == 0 {
		reply := tgbotapi.NewMessage(chatID, "Статистики пока нет")
		if _, err := b.api.Send(reply); err != nil {
			log.Printf("Error sending empty me reply: %v", err)
		}
		return
	}

	text := fmt.Sprintf("Всего сообщений в чате: %d\nУникальных пользователей, написавших сообщение: %d\nКоличество сообщений в чате за последние 24 часа: %d\n",
		stats.TotalMessages,
		stats.UniqueUsers,
		stats.MessagesLast24h,
	)
	reply := tgbotapi.NewMessage(chatID, text)
	if _, err := b.api.Send(reply); err != nil {
		log.Printf("send message: %v", err)
	}

}
