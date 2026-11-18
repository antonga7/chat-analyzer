package ai

import (
	"chat-analyzer/internal/storage"
	"context"
	"fmt"
	"log"
	"strings"

	"google.golang.org/genai"
)

type Client struct {
	genaiClient *genai.Client
	model       string
}

func New(ctx context.Context, apiKey string) (*Client, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("new client: %w", err)
	}

	return &Client{
		genaiClient: client,
		model:       "gemini-3.5-flash-lite",
	}, nil
}

func (c *Client) SummarizeChat(ctx context.Context, msgs []storage.Message) (string, error) {
	if len(msgs) == 0 {
		return "Нет текстовых сообщений для анализа.", nil
	}

	var sb strings.Builder
	_, err := sb.WriteString("Сделай краткую и структурированную выжимку (summary) переписки из чата.\n" +
		"Выдели главные темы, кто что предлагал и к чему пришли.\n\nСообщения:\n")
	if err != nil {
		return "", err
	}
	for _, msg := range msgs {
		_, _ = fmt.Fprintf(&sb, "[%s] %s: %s\n", msg.CreatedAt.Format("02.01 15:04"), msg.Username, msg.Text)
	}

	// Отправляем собранный текст (sb.String()) в модель.
	// genai.Text(...) оборачивает нашу обычную строку в формат контента, который понимает Gemini.
	// Последний аргумент nil — это доп. настройки (температура, лимит токенов), оставляем по умолчанию.
	resp, err := c.genaiClient.Models.GenerateContent(
		ctx,
		c.model,
		genai.Text(sb.String()),
		nil,
	)
	if err != nil {
		return "", fmt.Errorf("generate content: %w", err)
	}

	if resp.UsageMetadata != nil {
		log.Printf("Tokens used: prompt=%d, response=%d, total=%d",
			resp.UsageMetadata.PromptTokenCount,
			resp.UsageMetadata.CandidatesTokenCount,
			resp.UsageMetadata.TotalTokenCount)
	}

	// Метод resp.Text() достает готовый текстовый ответ ИИ из структуры ответа
	return resp.Text(), nil
}
