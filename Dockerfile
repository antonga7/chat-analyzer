# --- ЭТАП 1: Сборка (Стройка) ---
FROM golang:1.26-alpine AS builder

# Создаём папку /app внутри контейнера и заходим в неё
WORKDIR /app

# Копируем файлы зависимостей в текущую папку (./ означает /app)
COPY go.mod go.sum ./

# Скачиваем все библиотеки, перечисленные в go.mod
RUN go mod download

# Копируем весь код с Мака (первая точка) в /app контейнера (вторая точка)
COPY . .

# Компилируем проект в один файл /app/bot под Linux без привязки к C-библиотекам
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/bot ./cmd/bot/main.go

# --- ЭТАП 2: Финал (Чистый образ) ---
FROM alpine:latest

# Ставим SSL-сертификаты (чтобы работали HTTPS-запросы к Telegram/Gemini) и часовые пояса
RUN apk --no-cache add ca-certificates tzdata

# Снова заходим в /app уже в новом чистом образе
WORKDIR /app

# Вытаскиваем ТОЛЬКО скомпилированный файл из этапа builder в текущую папку (.)
COPY --from=builder /app/bot .

# Говорим контейнеру, какую команду выполнить при старте
CMD ["./bot"]