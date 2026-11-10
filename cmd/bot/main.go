package main

import (
	"chat-analyzer/internal/bot"
	"chat-analyzer/internal/storage"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("Application error: %v", err)
	}
}

func run() error {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		return errors.New("DB_DSN env variable not set")
	}

	botToken := os.Getenv("TG_BOT_TOKEN")
	if botToken == "" {
		return errors.New("TG_BOT_TOKEN env variable not set")
	}

	// Создаем контекст, который сам отменится при нажатии Ctrl+C (SIGINT) или остановке контейнера (SIGTERM)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Подключаемся к PostgreSQL
	db, err := storage.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer func() {
		log.Println("Closing database connection...")
		db.Close()
		log.Println("Database closed, Goodbye!")
	}()
	log.Println("Connected to database!")

	// Создаем таблицу и индекс, если их еще нет
	if err := db.InitSchema(ctx); err != nil {
		return fmt.Errorf("failed to init schema: %w", err)
	}
	log.Println("Database schema initialized!")

	// Инициализируем Telegram-бота
	tgBot, err := bot.New(botToken, db)
	if err != nil {
		return fmt.Errorf("failed to create bot: %w", err)
	}

	log.Println("Bot is running. Press Ctrl+C to stop.")
	tgBot.Start(ctx)
	log.Println("Bot stopped gracefully.")

	return nil
}