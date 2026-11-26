package importer

import (
	"chat-analyzer/internal/storage"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestExtractText(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "plain string",
			input:    `"Привет, чат!"`,
			expected: "Привет, чат!",
		},
		{
			name:     "mixed array with mention",
			input:    `["Привет, ", {"type": "mention", "text": "@anton"}, "!"]`,
			expected: "Привет, @anton!",
		},
		{
			name:     "empty input",
			input:    ``,
			expected: "",
		},
		{
			name:     "broken JSON",
			input:    `{broken`,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractText(json.RawMessage(tt.input))
			if got != tt.expected {
				t.Errorf("extractText() got = %q, expected %q", got, tt.expected)
			}
		})
	}
}

func TestParseJSON(t *testing.T) {
	const testChatID int64 = -100123456789

	tests := []struct {
		name     string
		input    string
		wantLen  int
		wantErr  bool
		wantType string
		wantText string
		wantUser string
		wantUID  int64
		wantTgID int64
	}{
		{
			name: "regular text message",
			input: `{
				"messages": [
					{
						"id": 42,
						"type": "message",
						"date_unixtime": "1700000000",
						"from": "Anton",
						"from_id": "user12345",
						"text": "Тестовое сообщение"
					}
				]
			}`,
			wantLen:  1,
			wantType: storage.TypeText,
			wantText: "Тестовое сообщение",
			wantUser: "Anton",
			wantUID:  12345,
			wantTgID: 42,
		},
		{
			name: "skip service and channel messages",
			input: `{
				"messages": [
					{"id": 1, "type": "service", "date_unixtime": "1700000000", "from": "Anton", "from_id": "user1", "text": "joined"},
					{"id": 2, "type": "message", "date_unixtime": "1700000000", "from": "News", "from_id": "channel999", "text": "post"}
				]
			}`,
			wantLen: 0,
		},
		{
			name:    "invalid json returns error",
			input:   `{not a valid json`,
			wantErr: true,
		},
		{
			name: "forwarded",
			input: `{
				"messages": [
					{
						"id": 4,
						"type": "message",
						"date": "2026-11-10T10:10:00",
						"date_unixtime": "1700000000",
						"from": "Anton",
						"from_id": "user254819201",
						"forwarded_from": "Go News Channel",
						"text": "Вышел релиз Go с ускоренным сборщиком мусора!"
					}
				]
			}`,
			wantType: storage.TypeForward,
			wantLen:  1,
			wantText: "Вышел релиз Go с ускоренным сборщиком мусора!",
			wantUser: "Anton",
			wantUID:  254819201,
			wantTgID: 4,
		},
		{
			name: "document",
			input: `{
				"messages": [
					{
						"id": 6,
      					"type": "message",
      					"date": "2026-11-10T10:20:00",
						"date_unixtime": "1700000000",
      					"from": "Anton",
      					"from_id": "user254819201",
      					"file": "(File not included. Change data exporting settings to download.)",
      					"file_name": "architecture.pdf",
      					"mime_type": "application/pdf",
      					"text": ""
					}
				]
			}`,
			wantType: storage.TypeDocument,
			wantText: "architecture.pdf",
			wantLen:  1,
			wantUser: "Anton",
			wantUID:  254819201,
			wantTgID: 6,
		},
		{
			name: "empty author name",
			input: `{"messages": [
					{
						"id": 42,
						"type": "message",
						"date_unixtime": "1700000000",
						"from": "",
						"from_id": "user12345",
						"text": "Тестовое сообщение"
					}
				]
			}`,
			wantType: storage.TypeText,
			wantUser: "Unknown",
			wantLen:  1,
			wantText: "Тестовое сообщение",
			wantUID:  12345,
			wantTgID: 42,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseJSON(strings.NewReader(tt.input), testChatID)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseJSON() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if len(got) != tt.wantLen {
				t.Fatalf("ParseJSON() got = %v, wantLen %v", len(got), tt.wantLen)
			}
			if tt.wantLen == 0 {
				return
			}

			msg := got[0]
			if msg.ChatID != testChatID {
				t.Errorf("ChatID = %d, want %d", msg.ChatID, testChatID)
			}
			if msg.MsgType != tt.wantType {
				t.Errorf("MsgType = %q, want %q", msg.MsgType, tt.wantType)
			}
			if msg.Text != tt.wantText {
				t.Errorf("Text = %q, want %q", msg.Text, tt.wantText)
			}
			if msg.Username != tt.wantUser {
				t.Errorf("Username = %q, want %q", msg.Username, tt.wantUser)
			}
			if msg.UserID != tt.wantUID {
				t.Errorf("UserID = %d, want %d", msg.UserID, tt.wantUID)
			}
			if msg.TgMessageID != tt.wantTgID {
				t.Errorf("TgMessageID = %d, want %d", msg.TgMessageID, tt.wantTgID)
			}
			if !msg.CreatedAt.Equal(time.Unix(1700000000, 0)) {
				t.Errorf("CreatedAt = %v, want %v", msg.CreatedAt, time.Unix(1700000000, 0))
			}
		})
	}
}
