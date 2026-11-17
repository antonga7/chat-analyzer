package bot

import (
	"chat-analyzer/internal/importer"
	"chat-analyzer/internal/storage"
	"context"
	"fmt"
	"log"
	"net/http"
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
			if update.Message == nil || update.Message.From == nil || update.Message.From.IsBot {
				continue
			}

			b.handleMessage(ctx, update.Message)

		}
	}
}

// classifyMessage определяет тип сообщения (текст, репост, фото, файл, медиа) и извлекает текст/подпись
func classifyMessage(msg *tgbotapi.Message) (msgType string, content string, ok bool) {
	// 1. Сначала проверяем, не пересланное ли это сообщение (репост)
	if msg.ForwardFrom != nil || msg.ForwardFromChat != nil || msg.ForwardSenderName != "" {
		text := msg.Text
		if text == "" {
			text = msg.Caption
		}
		return storage.TypeForward, text, true
	}

	// 2. Картинки / фото
	if len(msg.Photo) > 0 {
		return storage.TypePhoto, msg.Caption, true
	}

	// 3. Файлы / документы
	if msg.Document != nil {
		return storage.TypeDocument, msg.Document.FileName, true
	}

	// 4. Голосовые, видео, кружочки, стикеры, аудио
	if msg.Voice != nil || msg.Video != nil || msg.VideoNote != nil || msg.Sticker != nil || msg.Audio != nil {
		return storage.TypeMedia, msg.Caption, true
	}

	// 5. Обычный текст
	if msg.Text != "" {
		return storage.TypeText, msg.Text, true
	}

	return "", "", false

}

func (b *Bot) handleMessage(ctx context.Context, msg *tgbotapi.Message) {
	// Если прислали .json файл – перехватываем его для импорта истории чата (не сохраняя сам файл в статистику)
	if msg.Document != nil {
		filename := strings.ToLower(msg.Document.FileName)
		if strings.HasSuffix(filename, ".json") || strings.HasSuffix(filename, ".html") {
			b.handleFileImport(ctx, msg)
			return
		}

	}

	// Если это команда бота(/top, /me, /stats) - выполняем команду и не засоряем статистику

	if msg.IsCommand() {
		switch msg.Command() {
		case "start", "help":
			helpText := "👋 Привет! Я бот-анализатор чата.\n\nДоступные команды:\n/top — топ-5 активных участников\n/me — твоя личная статистика\n/stats — общая сводка по чату\n/help — список команд\n/import — как загрузить старую историю чата"
			reply := tgbotapi.NewMessage(msg.Chat.ID, helpText)
			if _, err := b.api.Send(reply); err != nil {
				log.Printf("sending message: %v", err)
			}
		case "import":
			importText := "📥 Как загрузить старую историю чата:\n\n" +
				"1. Открой этот чат в Telegram Desktop на компьютере.\n" +
				"2. Нажми «⋮» (три точки в правом верхнем углу) -> Экспорт истории чата.\n" +
				"3. Сними ВСЕ галочки с фото/видео/файлов.\n" +
				"4. В самом низу выбери Формат: «Машиночитаемый JSON».\n" +
				"5. Отправь полученный файл result.json прямо сюда в чат!"
			reply := tgbotapi.NewMessage(msg.Chat.ID, importText)
			if _, err := b.api.Send(reply); err != nil {
				log.Printf("sending import help: %v", err)
			}
		case "top":
			b.handleTopCommand(ctx, msg)
		case "me":
			b.handleMeCommand(ctx, msg)
		case "stats":
			b.handleStatsCommand(ctx, msg)
		}
		return
	}

	msgType, content, ok := classifyMessage(msg)
	if !ok {
		return
	}

	username := msg.From.UserName
	if username == "" {
		username = msg.From.FirstName // Если у юзера нет @ ника, берем его имая
	}

	dbMsg := storage.Message{
		ChatID:    msg.Chat.ID,
		UserID:    msg.From.ID,
		Username:  username,
		Text:      content,
		MsgType:   msgType,
		CreatedAt: msg.Time(),
	}

	if err := b.storage.SaveMessage(ctx, dbMsg); err != nil {
		log.Printf("save message: %v", err)
	}
}

func (b *Bot) handleTopCommand(ctx context.Context, msg *tgbotapi.Message) {
	stats, err := b.storage.GetTopUsers(ctx, msg.Chat.ID, 5)
	if err != nil {
		log.Printf("get top users: %v", err)
		return
	}

	if len(stats) == 0 {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "Пока нет сохраненных сообщений для статистики")
		if _, err := b.api.Send(reply); err != nil {
			log.Printf("send message: %v", err)
		}
		return
	}

	// Собираем красивый текст ответа через strings Builder
	var sb strings.Builder
	sb.WriteString("📊 Топ-5 активных участников чата:\n\n")
	for i, stat := range stats {
		fmt.Fprintf(&sb, "%d. %s — %d сообщ. (✍️ %d | 🔄 %d | 🖼 %d)\n",
			i+1, stat.Username, stat.TotalCount, stat.TextCount, stat.ForwardCount, stat.MediaCount)
	}
	reply := tgbotapi.NewMessage(msg.Chat.ID, sb.String())
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

	if meStat.TotalCount == 0 || meStat.FirstSeen == nil {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "Ты еще не написал ни одного сообщения в этом чате!")
		if _, err := b.api.Send(reply); err != nil {
			log.Printf("Error sending empty me reply: %v", err)
		}
		return
	}

	name := msg.From.UserName
	if name == "" {
		name = msg.From.FirstName
	}

	text := fmt.Sprintf(
		"👤 Статистика для %s:\n\n"+
			"📊 Всего активности: %d\n"+
			"✍️ Написал сам (текст): %d\n"+
			"🔄 Репостов: %d\n"+
			"🖼 Картинок: %d\n"+
			"📎 Файлов: %d\n"+
			"🎬 Голосовых / видео / стикеров: %d\n"+
			"📅 Первое сообщение: %s",
		name,
		meStat.TotalCount,
		meStat.TextCount,
		meStat.ForwardCount,
		meStat.PhotoCount,
		meStat.DocumentCount,
		meStat.MediaCount,
		meStat.FirstSeen.Format("02.01.2006 15:04"),
	)

	reply := tgbotapi.NewMessage(msg.Chat.ID, text)
	if _, err := b.api.Send(reply); err != nil {
		log.Printf("send message: %v", err)
	}
}

func (b *Bot) handleStatsCommand(ctx context.Context, msg *tgbotapi.Message) {
	stats, err := b.storage.GetChatStats(ctx, msg.Chat.ID)
	if err != nil {
		log.Printf("get stats: %v", err)
		return
	}

	if stats.TotalMessages == 0 {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "Статистики пока нет")
		if _, err := b.api.Send(reply); err != nil {
			log.Printf("Error sending empty me reply: %v", err)
		}
		return
	}

	text := fmt.Sprintf(
		"📈 Общая статистика чата:\n\n"+
			"👥 Участников в базе: %d\n"+
			"💬 Всего сообщений: %d\n\n"+
			"Из них:\n"+
			"✍️ Живой текст: %d\n"+
			"🔄 Репосты: %d\n"+
			"🖼 Картинки и мемы: %d\n"+
			"📎 Документы и файлы: %d\n"+
			"🎬 Видео / ГС / стикеры: %d",
		stats.UniqueUsers,
		stats.TotalMessages,
		stats.TextCount,
		stats.ForwardCount,
		stats.PhotoCount,
		stats.DocumentCount,
		stats.MediaCount,
	)

	reply := tgbotapi.NewMessage(msg.Chat.ID, text)
	if _, err := b.api.Send(reply); err != nil {
		log.Printf("send message: %v", err)
	}

}

func (b *Bot) handleFileImport(ctx context.Context, msg *tgbotapi.Message) {
	filename := strings.ToLower(msg.Document.FileName)

	// 1. Отправляем сообщение - статус
	statusMsg := tgbotapi.NewMessage(msg.Chat.ID, "⏳ Начинаю скачивание файла от серверов Telegram...")
	sentStatus, _ := b.api.Send(statusMsg)

	// 2. Запрашиваем у Telegram прямую ссылка на скачивание файла по его FileId
	fileURL, err := b.api.GetFileDirectURL(msg.Document.FileID)
	if err != nil {
		if _, err := b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ Ошибка получения ссылки на файл.")); err != nil {
			log.Printf("get file direct url: %v", err)
		}
		return
	}

	// 3. Скачиваем файл в оперативную память
	resp, err := http.Get(fileURL)
	if err != nil {
		if _, err := b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ Ошибка скачивания файла.")); err != nil {
			log.Printf("send error: %v", err)
		}

		return
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("error closing body: %v", err)
		}
	}()

	// Обновляем статус
	edit := tgbotapi.NewEditMessageText(msg.Chat.ID, sentStatus.MessageID, "⏳ Файл скачан. Идет парсинг сообщений...")
	if _, err := b.api.Send(edit); err != nil {
		log.Printf("send error: %v", err)
	}

	var msgs []storage.Message
	var parseErr error

	if strings.HasSuffix(filename, ".json") {
		msgs, parseErr = importer.ParseJSON(resp.Body, msg.Chat.ID)
	}
	if strings.HasSuffix(filename, ".html") {
		msgs, parseErr = importer.ParseHTML(resp.Body, msg.Chat.ID)
	}

	// Обработка ошибок парсинга
	if parseErr != nil {
		errText := fmt.Sprintf("❌ Ошибка при чтении файла экспорта: %v", parseErr)
		if _, err := b.api.Send(tgbotapi.NewEditMessageText(msg.Chat.ID, sentStatus.MessageID, errText)); err != nil {
			log.Printf("send error: %v", err)
		}
		return
	}
	if len(msgs) == 0 {
		if _, err := b.api.Send(tgbotapi.NewEditMessageText(msg.Chat.ID, sentStatus.MessageID, "В файле не найдено сообщений.")); err != nil {
			log.Printf("send error: %v", err)
		}
		return
	}

	// Обновляем стату
	edit = tgbotapi.NewEditMessageText(msg.Chat.ID, sentStatus.MessageID, fmt.Sprintf("⏳ Найдено %d сообщений. Сохраняю в базу данных (это займет пару секунд)...", len(msgs)))
	if _, err := b.api.Send(edit); err != nil {
		log.Printf("send error: %v", err)
	}

	if err = b.storage.SaveMessagesBatch(ctx, msgs); err != nil {
		edit = tgbotapi.NewEditMessageText(msg.Chat.ID, sentStatus.MessageID, fmt.Sprintf("❌ Ошибка сохранения в БД: %v", err))
		if _, err := b.api.Send(edit); err != nil {
			log.Printf("send error: %v", err)
		}
		return
	}

	successText := fmt.Sprintf("✅ Готово! Успешно загружено %d сообщений в базу.\nТеперь можно использовать команду /top или /stats", len(msgs))
	if _, err := b.api.Send(tgbotapi.NewEditMessageText(msg.Chat.ID, sentStatus.MessageID, successText)); err != nil {
		log.Printf("send error: %v", err)
	}

}
