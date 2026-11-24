package ai

import (
	"chat-analyzer/internal/storage"
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"google.golang.org/genai"
)

type SummaryStyle string

const (
	StyleOfficial SummaryStyle = "official"
	StyleFunStory SummaryStyle = "fun_story"
	maxRetries                 = 3
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

func (c *Client) SummarizeChat(ctx context.Context, msgs []storage.Message, style SummaryStyle) (string, error) {
	if len(msgs) == 0 {
		return "Нет текстовых сообщений для анализа.", nil
	}

	var sb strings.Builder

	var prompt string

	switch style {
	case StyleFunStory:
		prompt = "Ты — остроумный рассказчик и стендап-комик. " +
			"Перескажи последние сообщения из чата в виде короткого ироничного мини-рассказа (как трейлер к драматичному фильму или байку у костра). " +
			"Обыгрывай имена участников и их реплики с добрым юмором, подколками, без оскорблений и душных списков с буллитами. " +
			"Объём — 2–4 абзаца.\n\nСообщения:\n"
	default:
		prompt = "Сделай краткую и структурированную выжимку (summary) переписки из чата.\n" +
			"Выдели главные темы, кто что предлагал и к чему пришли.\n\nСообщения:\n"
	}
	_, err := sb.WriteString(prompt)
	if err != nil {
		return "", err
	}

	for _, msg := range msgs {
		_, _ = fmt.Fprintf(&sb, "[%s] %s: %s\n", msg.CreatedAt.Format("02.01 15:04"), msg.Username, msg.Text)
	}

	// Отправляем собранный текст (sb.String()) в модель.
	// genai.Text(...) оборачивает нашу обычную строку в формат контента, который понимает Gemini.
	// Последний аргумент nil — это доп. настройки (температура, лимит токенов), оставляем по умолчанию.
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		resp, err := c.genaiClient.Models.GenerateContent(
			ctx,
			c.model,
			genai.Text(sb.String()),
			nil,
		)
		if err == nil {
			if resp.UsageMetadata != nil {
				log.Printf("Tokens used: prompt=%d, response=%d, total=%d",
					resp.UsageMetadata.PromptTokenCount,
					resp.UsageMetadata.CandidatesTokenCount,
					resp.UsageMetadata.TotalTokenCount)
			}
			// Метод resp.Text() достает готовый текстовый ответ ИИ из структуры ответа
			return resp.Text(), nil
		}

		lastErr = err
		if !isRetryable(lastErr) {
			return "", fmt.Errorf("generate content: %w", err)
		}

		if attempt == maxRetries {
			break
		}

		log.Printf("Gemini temporary error (attempt %d/%d): %v. Retrying...", attempt, maxRetries, err)

		delay := time.Duration(attempt*2) * time.Second

		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}

	return "", fmt.Errorf("generate content after %d attempts: %w", maxRetries, lastErr)

}



func isRetryable(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "503") ||
		strings.Contains(msg, "UNAVAILABLE") ||
		strings.Contains(msg, "429")
}
