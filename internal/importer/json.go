package importer

import (
	"chat-analyzer/internal/storage"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// exportData описывает корень файла result.json
type exportData struct {
	Messages []telegramMessage `json:"messages"`
}

// rawMessage описывает одно сообщение внутри JSON
type telegramMessage struct {
	Type          string          `json:"type"`           // "message" или "service"
	DateUnix      string          `json:"date_unixtime"`  // Например:"1794305112"
	From          string          `json:"from"`           // Имя автора ("Anton")
	FromID        string          `json:"from_id"`        // Строка вида "user254819201"
	ID            int64           `json:"id"`             // ID сообщения
	ForwardedFrom string          `json:"forwarded_from"` // Заполнено, если это репост
	Photo         string          `json:"photo"`          // Заполнено, если есть фото
	File          string          `json:"file"`           // Заполнено, если прикреплен файл/медиа
	FileName      string          `json:"file_name"`      // Название документа ("architecture.pdf")/
	MediaType     string          `json:"media_type"`     // "voice_message", "video_file", "sticker"...
	Text          json.RawMessage `json:"text"`           // Сырые байты: может быть строкой "..." или массивом [...]
}

func extractText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		return str
	}

	var arr []any
	if err := json.Unmarshal(raw, &arr); err != nil {
		return ""
	}

	var sb strings.Builder

	for _, item := range arr {
		switch v := item.(type) {
		case string:
			sb.WriteString(v)
		case map[string]any:
			if textVal, ok := v["text"].(string); ok {
				sb.WriteString(textVal)
			}
		}
	}
	return sb.String()
}

func classifyExporting(tm telegramMessage) (msgType string, content string, ok bool) {
	text := extractText(tm.Text)

	if tm.ForwardedFrom != "" {
		return storage.TypeForward, text, true
	}
	if tm.Photo != "" {
		return storage.TypePhoto, text, true
	}
	if tm.MediaType != "" {
		return storage.TypeMedia, text, true
	}
	if tm.File != "" {
		if tm.FileName != "" {
			return storage.TypeDocument, tm.FileName, true // Сохраним имя файла в базу!
		}
		return storage.TypeDocument, text, true
	}
	if text != "" {
		return storage.TypeText, text, true
	}

	return "", "", false

}

func ParseJSON(r io.Reader, chatID int64) ([]storage.Message, error) {
	var data exportData
	if err := json.NewDecoder(r).Decode(&data); err != nil {
		return nil, fmt.Errorf("decode json: %w", err)
	}
	result := make([]storage.Message, 0, len(data.Messages))
	for _, msg := range data.Messages {
		if msg.Type != "message" || !strings.HasPrefix(msg.FromID, "user") {
			continue
		}

		idStr := strings.TrimPrefix(msg.FromID, "user")
		userID, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			continue
		}
		sec, err := strconv.ParseInt(msg.DateUnix, 10, 64)
		if err != nil {
			continue
		}

		createdAt := time.Unix(sec, 0)

		msgType, content, ok := classifyExporting(msg)
		if !ok {
			continue
		}

		username := msg.From
		if username == "" {
			username = "Unknown"
		}

		result = append(result, storage.Message{
			ChatID:      chatID,
			UserID:      userID,
			Username:    username,
			Text:        content,
			MsgType:     msgType,
			CreatedAt:   createdAt,
			TgMessageID: msg.ID,
		})

	}
	return result, nil
}
