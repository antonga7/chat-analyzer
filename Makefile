.PHONY: run build lint test db-up db-down

# Запуск бота локально
run:
	go run ./cmd/bot/main.go

# Сборка бинарника
build:
	go build -o bin/bot ./cmd/bot/main.go

# Проверка кода линтером
lint:
	golangci-lint run

# Запуск всех тестов
test:
	go test -v ./...

# Поднять базу данных в фоне
db-up:
	docker compose up -d

# Остановить базу данных
db-down:
	docker compose down