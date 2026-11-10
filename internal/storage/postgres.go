package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Storage struct {
	pool *pgxpool.Pool
}

func New(ctx context.Context, dsn string) (*Storage, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := pool.Ping(pingCtx); err != nil {
		return nil, fmt.Errorf("unable to ping database: %w", err)
	}
	return &Storage{pool: pool}, nil
}

func (s *Storage) Close() {
	s.pool.Close()
}

type Message struct {
	ID        int64
	ChatID    int64
	UserID    int64
	Username  string
	Text      string
	CreatedAt time.Time
}

func (s *Storage) InitSchema(ctx context.Context) error {
	query := `
	CREATE TABLE IF NOT EXISTS messages (
    	id BIGSERIAL PRIMARY KEY,
    	chat_id bigint NOT NULL,
    	user_id bigint NOT NULL,
    	username TEXT NOT NULL,
    	text TEXT NOT NULL,
    	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);
	CREATE INDEX IF NOT EXISTS idx_messages_chat_created ON messages (chat_id, created_at);
	`
	
	_, err := s.pool.Exec(ctx, query)
	if err != nil {
		return fmt.Errorf("unable to create messages table: %w", err)
	}

	return nil
}

func (s *Storage) SaveMessage(ctx context.Context, msg Message) error {
	query := `
	INSERT INTO messages (chat_id, user_id, username, text)
	VALUES ($1, $2, $3, $4);`

	_, err := s.pool.Exec(ctx, query, msg.ChatID, msg.UserID, msg.Username, msg.Text)
	if err != nil {
		return fmt.Errorf("unable to save message: %w", err)

	}

	return nil
}

type UserStat struct {
	Username     string
	MessageCount int
}

func (s *Storage) GetTopUsers(ctx context.Context, chatID int64, limit int) ([]UserStat, error) {
	query := `
	SELECT username, COUNT(*) as msg_count
	FROM messages
	WHERE chat_id = $1
	GROUP BY username
	ORDER BY msg_count DESC
	LIMIT $2;`

	rows, err := s.pool.Query(ctx, query, chatID, limit)
	if err != nil {
		return nil, fmt.Errorf("unable to get top users: %w", err)
	}
	defer rows.Close()

	var stats []UserStat

	for rows.Next() {
		var stat UserStat
		if err := rows.Scan(&stat.Username, &stat.MessageCount); err != nil {
			return nil, fmt.Errorf("unable to get top users: %w", err)
		}
		stats = append(stats, stat)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}
	return stats, nil
}

type PersonalStat struct {
	MessageCount int
	FirstSeen    time.Time
}

func (s *Storage) GetUserStats(ctx context.Context, chatID int64, userID int64) (PersonalStat, error) {
	query := `
	SELECT COUNT(*), COALESCE(MIN(created_at), NOW())
	FROM messages
	WHERE chat_id = $1 AND user_id = $2;
	`

	var stat PersonalStat
	err := s.pool.QueryRow(ctx, query, chatID, userID).Scan(&stat.MessageCount, &stat.FirstSeen)
	if err != nil {
		return PersonalStat{}, fmt.Errorf("unable to get user stats: %w", err)
	}
	return stat, nil
}

type ChatStat struct {
	TotalMessages   int
	UniqueUsers     int
	MessagesLast24h int
}

func (s *Storage) GetChatStats(ctx context.Context, chatID int64) (ChatStat, error) {
	query := `
	SELECT COUNT(*), 
	       COUNT(DISTINCT user_id), 
	       COUNT(*) FILTER (WHERE created_at >= NOW() - INTERVAL '24 hours')
	FROM messages
	WHERE chat_id = $1;
	`

	var stat ChatStat
	err := s.pool.QueryRow(ctx, query, chatID).Scan(&stat.TotalMessages, &stat.UniqueUsers, &stat.MessagesLast24h)
	if err != nil {
		return ChatStat{}, fmt.Errorf("unable to get chat stats: %w", err)
	}
	return stat, nil
}
