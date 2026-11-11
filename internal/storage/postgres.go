package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Константы типов сообщений
const (
	TypeText     = "text"     // Обычное текстовое сообщение
	TypeForward  = "forward"  // Репост (пересланное сообщение)
	TypePhoto    = "photo"    // Картинка / фото
	TypeDocument = "document" // Файл / документ
	TypeMedia    = "media"    // Видео, кружочек, голосовое, стикер
)

type Storage struct {
	pool *pgxpool.Pool
}

func New(ctx context.Context, dsn string) (*Storage, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("unable to create connection pool: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
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
	MsgType   string
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
    	msg_type TEXT NOT NULL DEFAULT 'text',
    	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);
	ALTER TABLE messages ADD COLUMN IF NOT EXISTS msg_type TEXT NOT NULL DEFAULT 'text';
	CREATE INDEX IF NOT EXISTS idx_messages_chat_created ON messages (chat_id, created_at);
	`

	_, err := s.pool.Exec(ctx, query)
	if err != nil {
		return fmt.Errorf("unable to create messages table: %w", err)
	}

	return nil
}

func (s *Storage) SaveMessage(ctx context.Context, msg Message) error {
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now()
	}

	if msg.MsgType == "" {
		msg.MsgType = TypeText
	}
	query := `
	INSERT INTO messages (chat_id, user_id, username, text, msg_type, created_at)
	VALUES ($1, $2, $3, $4, $5, $6);`

	_, err := s.pool.Exec(ctx, query, msg.ChatID, msg.UserID, msg.Username, msg.Text, msg.MsgType, msg.CreatedAt)
	if err != nil {
		return fmt.Errorf("unable to save message: %w", err)

	}

	return nil
}

type UserStat struct {
	Username     string
	TotalCount   int
	TextCount    int
	ForwardCount int
	MediaCount   int
}

func (s *Storage) GetTopUsers(ctx context.Context, chatID int64, limit int) ([]UserStat, error) {
	query := `
	SELECT 
	    Max(username) as username, 
	    COUNT(*) as total_count,
		COUNT(*) FILTER(WHERE msg_type = 'text') as text_count,
		COUNT(*) FILTER(WHERE msg_type = 'forward') as forward_count,
		COUNT(*) FILTER(WHERE msg_type IN ('photo', 'document', 'media')) as media_count
	FROM messages
	WHERE chat_id = $1
	GROUP BY user_id
	ORDER BY total_count DESC
	LIMIT $2;`

	rows, err := s.pool.Query(ctx, query, chatID, limit)
	if err != nil {
		return nil, fmt.Errorf("unable to get top users: %w", err)
	}
	defer rows.Close()

	var stats []UserStat

	for rows.Next() {
		var stat UserStat
		if err := rows.Scan(&stat.Username, &stat.TotalCount,&stat.TextCount, &stat.ForwardCount, &stat.MediaCount); err != nil {
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
	TotalCount    int
	TextCount     int
	ForwardCount  int
	PhotoCount    int
	DocumentCount int
	MediaCount    int
	FirstSeen     *time.Time
}

func (s *Storage) GetUserStats(ctx context.Context, chatID int64, userID int64) (PersonalStat, error) {
	query := `
	SELECT 
		COUNT(*),
		COUNT(*) FILTER (WHERE msg_type = 'text'),
		COUNT(*) FILTER (WHERE msg_type = 'forward'),
		COUNT(*) FILTER (WHERE msg_type = 'photo'),
		COUNT(*) FILTER (WHERE msg_type = 'document'),
		COUNT(*) FILTER (WHERE msg_type = 'media'),
		MIN(created_at)
	FROM messages
	WHERE chat_id = $1 AND user_id = $2;`

	var st PersonalStat
	err := s.pool.QueryRow(ctx, query, chatID, userID).Scan(
		&st.TotalCount,
		&st.TextCount,
		&st.ForwardCount,
		&st.PhotoCount,
		&st.DocumentCount,
		&st.MediaCount,
		&st.FirstSeen,
	)
	if err != nil {
		return PersonalStat{}, fmt.Errorf("unable to get user stats: %w", err)
	}

	return st, nil
}

type ChatStat struct {
	TotalMessages int
	UniqueUsers   int
	TextCount     int
	ForwardCount  int
	PhotoCount    int
	DocumentCount int
	MediaCount    int
}

func (s *Storage) GetChatStats(ctx context.Context, chatID int64) (ChatStat, error) {
	query := `
	SELECT 
		COUNT(*),
		COUNT(DISTINCT user_id),
		COUNT(*) FILTER (WHERE msg_type = 'text'),
		COUNT(*) FILTER (WHERE msg_type = 'forward'),
		COUNT(*) FILTER (WHERE msg_type = 'photo'),
		COUNT(*) FILTER (WHERE msg_type = 'document'),
		COUNT(*) FILTER (WHERE msg_type = 'media')
	FROM messages
	WHERE chat_id = $1;`

	var st ChatStat
	err := s.pool.QueryRow(ctx, query, chatID).Scan(
		&st.TotalMessages,
		&st.UniqueUsers,
		&st.TextCount,
		&st.ForwardCount,
		&st.PhotoCount,
		&st.DocumentCount,
		&st.MediaCount,
	)
	if err != nil {
		return ChatStat{}, fmt.Errorf("unable to get chat stats: %w", err)
	}

	return st, nil
}
