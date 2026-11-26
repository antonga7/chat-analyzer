# Chat Analyzer Bot

Telegram-бот на Go для сбора статистики групповых чатов, импорта истории переписки и генерации ИИ-саммари с помощью Google Gemini API.

## Возможности

- **Сбор и аналитика сообщений:** учёт активности участников по типам контента (текст, репосты, фото, документы, медиа) и вывод личной и общей статистики чата.
- **ИИ-саммаризация:** асинхронная генерация кратких сводок (`/summary`) и ироничных мини-рассказов (`/fun_story`) по последним сообщениям через Google Gemini API с очередью задач и rate limiting.
- **Импорт истории Telegram:** загрузка и парсинг экспортов чатов в форматах **JSON** (`result.json`) и **HTML** (`messages.html`, с поддержкой форматов дат Telegram Desktop и macOS).
- **Быстрая пакетная вставка и дедупликация:** загрузка тысяч сообщений в PostgreSQL через бинарный протокол `COPY` (`pgx.CopyFrom`) во временную таблицу с последующим слиянием через `NULLIF` и `ON CONFLICT DO NOTHING`.
- **Отказоустойчивость:** ретраи при временных ошибках Gemini API (`429` / `503`), разделение ошибок между слоями (`errors.AsType` / `errors.Is`) и graceful shutdown по сигналам `SIGINT` / `SIGTERM`.

## Команды бота

- `/top` — топ-5 активных участников чата
- `/me` — личная статистика пользователя
- `/stats` — общая статистика по чату
- `/summary` — структурированная ИИ-выжимка последних сообщений
- `/fun_story` — шуточный пересказ последних обсуждений
- `/import` — инструкция по загрузке истории чата (JSON / HTML)
- `/help` — список доступных команд

## Стек технологий

- **Язык:** Go 1.26
- **База данных:** PostgreSQL 16, `jackc/pgx/v5` (`pgxpool`)
- **Telegram API:** `go-telegram-bot-api/v5`
- **ИИ:** Google Gemini API (`google.golang.org/genai`)
- **Парсинг HTML:** `golang.org/x/net/html`
- **Инфраструктура:** Docker, Docker Compose, Makefile, `golangci-lint`

## Структура проекта

```text
.
├── cmd/bot/             # Точка входа, инициализация зависимостей и graceful shutdown
├── internal/
│   ├── ai/              # Клиент Gemini API с ретраями и типизированными ошибками
│   ├── bot/             # Обработчики Telegram-команд, импорт файлов, очередь и воркер ИИ
│   ├── importer/        # Парсеры JSON и HTML экспортов Telegram (unit-тесты, 93.1% coverage)
│   └── storage/         # Схема БД, составные/уникальные индексы, pgxpool и batch-импорт
├── docker-compose.yml   # Оркестрация контейнеров бота и PostgreSQL
├── Dockerfile           # Многоэтапная сборка минимального образа (alpine)
└── Makefile             # Команды для разработки, тестов, линтера и деплоя
```

## Запуск проекта

### 1. Настройка окружения

Создайте файл `.env` в корне проекта:

```env
TG_BOT_TOKEN=your_telegram_bot_token
GEMINI_API_KEY=your_gemini_api_key
DB_DSN=postgres://bot_user:bot_password@localhost:5432/chat_analyzer
```

*(При запуске через Docker Compose переменная `DB_DSN` автоматически переопределяется внутри контейнера для подключения к сервису `postgres`.)*

### 2. Запуск через Docker Compose

```bash
docker compose up -d --build
```

Бот автоматически создаст необходимые таблицы и индексы в PostgreSQL при первом старте.

## Разработка и тестирование

Основные команды вынесены в `Makefile`:

```bash
make run     # Локальный запуск бота
make build   # Сборка бинарника в bin/bot
make test    # Запуск юнит-тестов с детектором гонок (-race) и покрытием (-cover)
make lint    # Статический анализ кода через golangci-lint
make db-up   # Поднять контейнеры в фоне
make db-down # Остановить контейнеры
make ssh     # Подключение к удалённому серверу по SSH
make deploy  # Деплой и пересборка контейнеров на удалённом сервере
```