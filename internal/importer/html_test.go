package importer

import (
	"chat-analyzer/internal/storage"
	"strings"
	"testing"
	"time"
)

func TestParseExportDate(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantYear  int
		wantMonth time.Month
		wantDay   int
		wantOK    bool
	}{
		{
			name:      "telegram desktop format",
			input:     "03.11.2026 00:36:15",
			wantYear:  2026,
			wantMonth: time.November,
			wantDay:   3,
			wantOK:    true,
		},
		{
			name:      "date with month in english",
			input:     "5 November 2026, 00:36:15",
			wantYear:  2026,
			wantMonth: time.November,
			wantDay:   5,
			wantOK:    true,
		},
		{
			name:      "date with month in russian",
			input:     "5 ноября 2026, 00:36:15",
			wantYear:  2026,
			wantMonth: time.November,
			wantDay:   5,
			wantOK:    true,
		},
		{
			name:   "empty or wrong string",
			input:  "not a date",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseExportDate(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("parseExportDate(%q) ok = %v, wantOK %v", tt.input, ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if got.Year() != tt.wantYear || got.Month() != tt.wantMonth || got.Day() != tt.wantDay {
				t.Errorf("parseExportDate(%q) = %v, want %04d-%02d-%02d", tt.input, got, tt.wantYear, tt.wantMonth, tt.wantDay)
			}
		})
	}
}

func TestParseHTML(t *testing.T) {
	const testChatID int64 = -100123456789

	type wantMsg struct {
		username string
		userID   int64
		tgID     int64
		msgType  string
		text     string
	}

	tests := []struct {
		name  string
		input string
		want  []wantMsg
	}{
		{
			name: "regular text with avatar author id",
			input: `<html><body>
				<div class="message default clearfix" id="message101">
					<img class="userpic" src="photos/author_12345.jpg" />
					<div class="body">
						<div class="pull_right date details" title="03.11.2026 12:00:00">12:00</div>
						<div class="from_name">Anton</div>
						<div class="text">Привет<br/>из HTML</div>
					</div>
				</div>
			</body></html>`,
			want: []wantMsg{
				{
					username: "Anton",
					userID:   12345,
					tgID:     101,
					msgType:  storage.TypeText,
					text:     "Привет\nиз HTML",
				},
			},
		},
		{
			name: "joined message inherits previous author",
			input: `<html><body>
				<div class="message default clearfix" id="message201">
					<div class="body">
						<div class="pull_right date details" title="03.11.2026 12:00:00">12:00</div>
						<div class="from_name">Yulia</div>
						<div class="text">Первое</div>
					</div>
				</div>
				<div class="message default clearfix joined" id="message202">
					<div class="body">
						<div class="pull_right date details" title="03.11.2026 12:01:00">12:01</div>
						<div class="text">Второе</div>
					</div>
				</div>
			</body></html>`,
			want: []wantMsg{
				{username: "Yulia", tgID: 201, msgType: storage.TypeText, text: "Первое"},
				{username: "Yulia", tgID: 202, msgType: storage.TypeText, text: "Второе"},
			},
		},
		{
			name: "skip service and message without date",
			input: `<html><body>
				<div class="message service" id="message1">
					<div class="body details">3 November 2026</div>
				</div>
				<div class="message default clearfix" id="message2">
					<div class="body">
						<div class="from_name">Anton</div>
						<div class="text">Без даты</div>
					</div>
				</div>
			</body></html>`,
			want: nil,
		},
		{
			name: "forwarded message",
			input: `<html><body>
				<div class="message default clearfix" id="message301">
					<div class="body">
						<div class="pull_right date details" title="03.11.2026 12:05:00">12:05</div>
						<div class="from_name">Anton</div>
						<div class="forwarded body">
							<div class="from_name">Go News</div>
							<div class="text">Вышел Go 1.26</div>
						</div>
					</div>
				</div>
			</body></html>`,
			want: []wantMsg{
				{username: "Anton", tgID: 301, msgType: storage.TypeForward, text: "Вышел Go 1.26"},
			},
		},
		{
			name: "photo message",
			input: `<html><body>
				<div class="message default clearfix" id="message302">
					<div class="body">
						<div class="pull_right date details" title="03.11.2026 12:10:00">12:10</div>
						<div class="from_name">Yulia</div>
						<div class="photo_wrap clearfix"></div>
					</div>
				</div>
			</body></html>`,
			want: []wantMsg{
				{username: "Yulia", tgID: 302, msgType: storage.TypePhoto, text: ""},
			},
		},
		{
			name: "document message",
			input: `<html><body>
				<div class="message default clearfix" id="message303">
					<div class="body">
						<div class="pull_right date details" title="03.11.2026 12:15:00">12:15</div>
						<div class="from_name">Anton</div>
						<div class="media_wrap clearfix">
							<div class="media clearfix pull_left media_file"></div>
						</div>
					</div>
				</div>
			</body></html>`,
			want: []wantMsg{
				{username: "Anton", tgID: 303, msgType: storage.TypeDocument, text: ""},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseHTML(strings.NewReader(tt.input), testChatID)
			if err != nil {
				t.Fatalf("ParseHTML(%q): %v", tt.input, err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("len(got) = %d, want %d", len(got), len(tt.want))
			}

			for i, w := range tt.want {
				msg := got[i]
				if msg.ChatID != testChatID {
					t.Errorf("[%d].ChatID = %d, want %d", i, msg.ChatID, testChatID)
				}
				if msg.Username != w.username {
					t.Errorf("[%d] Username = %q, want %q", i, msg.Username, w.username)
				}
				if w.userID != 0 && msg.UserID != w.userID {
					t.Errorf("[%d] UserID = %d, want %d", i, msg.UserID, w.userID)
				}
				if w.userID == 0 && msg.UserID == 0 {
					t.Errorf("[%d] UserID = 0, want non-zero hashed ID", i)
				}
				if msg.TgMessageID != w.tgID {
					t.Errorf("[%d] TgMessageID = %d, want %d", i, msg.TgMessageID, w.tgID)
				}
				if msg.MsgType != w.msgType {
					t.Errorf("[%d] MsgType = %q, want %q", i, msg.MsgType, w.msgType)
				}
				if msg.Text != w.text {
					t.Errorf("[%d] Text = %q, want %q", i, msg.Text, w.text)
				}
			}
		})
	}
}
