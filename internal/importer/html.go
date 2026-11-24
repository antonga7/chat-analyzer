package importer

import (
	"chat-analyzer/internal/storage"
	"fmt"
	"hash/fnv"
	"io"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// 1. getAttr ищет значения атрибута (например "class", "title", "src") у HTML-тега
func getAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// 2. ExtractNodeText собирает весь чистый текст внутри тега (например, внутри <div class="text">,
// превращая теги переноса строки <br> в настоящий "\n"
func extractNodeText(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			sb.WriteString(node.Data)
		} else if node.Type == html.ElementNode && node.Data == "br" {
			sb.WriteString("\n")
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.TrimSpace(sb.String())
}

var monthsMap = map[string]string{
	// Русские (родительный падеж, как в macOS Telegram)
	"января": "01", "февраля": "02", "марта": "03", "апреля": "04",
	"мая": "05", "июня": "06", "июля": "07", "августа": "08",
	"сентября": "09", "октября": "10", "ноября": "11", "декабря": "12",
	// Английские
	"January": "01", "February": "02", "March": "03", "April": "04",
	"May": "05", "June": "06", "July": "07", "August": "08",
	"September": "09", "October": "10", "November": "11", "December": "12",
}

func parseExportDate(raw string) (time.Time, bool) {
	// Вариант 1: формат Telegram Desktop ("03.11.2026 00:36:15")
	if t, err := time.Parse("02.01.2006 15:04:05", raw); err == nil {
		return t, true
	}

	// Вариант 2: формат macOS Telegram ("3 ноября 2026, 00:36:15" или "3 November 2026, 00:36:15")
	normalized := raw
	for word, num := range monthsMap {
		if strings.Contains(normalized, word) {
			normalized = strings.Replace(normalized, word, num, 1)
			break
		}
	}
	if t, err := time.Parse("2 01 2006, 15:04:05", normalized); err == nil {
		return t, true
	}

	return time.Time{}, false
}

// 3. hashUsername генерирует стабильный числовой ID из имени, если у юзера нет аватарки author_XXX.jpg
func hashUsername(name string) int64 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	return int64(-h.Sum32())
}

// 4. findNodeByClass рекурсивно ищет тег, у которого в class есть искомая подстрока
func findNodeByClass(n *html.Node, className string) *html.Node {
	if n.Type == html.ElementNode && strings.Contains(getAttr(n, "class"), className) {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findNodeByClass(c, className); found != nil {
			return found
		}
	}
	return nil
}

func ParseHTML(r io.Reader, chatID int64) ([]storage.Message, error) {
	// 1. Превращаем поток в дерево HTML-узлов
	doc, err := html.Parse(r)
	if err != nil {
		return nil, fmt.Errorf("parse html: %w", err)
	}

	var result []storage.Message

	// Эти переменные помнят автора для сообщений, идущих подряд (class="joined")
	var lastAuthor string
	var lastAuthorID int64

	// 2. Функция для рекурсивного обхода всего документа
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		// Ищем <div> с классом "message default" (наши сообщения)
		if n.Type == html.ElementNode && n.Data == "div" {
			class := getAttr(n, "class")
			if strings.Contains(class, "message default") {

				// --- ШАГ 1: АВТОР И ЕГО ID ---
				author := lastAuthor
				authorID := lastAuthorID

				// ID сообщений
				var msgID int64
				if rawID := getAttr(n, "id"); rawID != ""{
					if parsedID, err := strconv.ParseInt(strings.TrimPrefix(rawID, "message"), 10, 64); err == nil {
						msgID = parsedID
					}
				}


				// Если сообщение НЕ joined, значит автор сменился — достаем нового
				if !strings.Contains(class, "joined") {
					nameNode := findNodeByClass(n, "from_name")
					if nameNode != nil {
						author = extractNodeText(nameNode)
						authorID = hashUsername(author) // Базовый ID через хэш

						// Пытаемся вытащить реальный ID из картинки (photos/author_292503924.jpg)
						picNode := findNodeByClass(n, "userpic")
						if picNode != nil && picNode.Data == "img" {
							src := getAttr(picNode, "src")
							if strings.Contains(src, "author_") {
								idStr := strings.TrimSuffix(strings.Split(src, "author_")[1], ".jpg")
								if idNum, err := strconv.ParseInt(idStr, 10, 64); err == nil {
									authorID = idNum
								}
							}
						}
					}
					// Запоминаем для следующих joined-сообщений
					lastAuthor = author
					lastAuthorID = authorID
				}

				// --- ШАГ 2: ДАТА ---
				var createdAt time.Time
				dateNode := findNodeByClass(n, "date details")
				if dateNode != nil {
					rawDate := getAttr(dateNode, "title")  // "3 ноября 2026, 00:36:15"
					parsed, ok := parseExportDate(rawDate) // Используем нашу функцию с bool!
					if ok {
						createdAt = parsed
					}
				}

				// --- ШАГ 3: ТЕКСТ СООБЩЕНИЯ ---
				var content string
				textNode := findNodeByClass(n, "text")
				if textNode != nil {
					content = extractNodeText(textNode)
				}

				// КЛАССИФИКАЦИЯ (msgType)
				var msgType string

				switch {
				case findNodeByClass(n, "forwarded body") != nil:
					msgType = storage.TypeForward
				case findNodeByClass(n, "media_photo") != nil || findNodeByClass(n, "photo_wrap") != nil:
					msgType = storage.TypePhoto
				case findNodeByClass(n, "media_voice_message") != nil || findNodeByClass(n, "media_video") != nil:
					msgType = storage.TypeMedia
				case findNodeByClass(n, "media_file") != nil:
					msgType = storage.TypeDocument
				case content != "":
					msgType = storage.TypeText
				}

				// Если msgType остался пустой строкой (не удалось определить тип)
				// или у сообщения нет даты (createdAt.IsZero()) — делаем просто return (выходим из текущего шага обхода)
				if msgType == "" || createdAt.IsZero() {
					return
				}

				if author == "" {
					author = "Unknown"
				}

				// Собираем структуру storage.Message и сделай result = append(result, msg)
				result = append(result, storage.Message{
					ChatID:      chatID,
					UserID:      authorID,
					Username:    author,
					Text:        content,
					MsgType:     msgType,
					CreatedAt:   createdAt,
					TgMessageID: msgID,
				})
			}
		}

		// Продолжаем идти по детям узла
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}

	// 3. Запускаем обход с самого корня документа
	walk(doc)

	return result, nil
}
