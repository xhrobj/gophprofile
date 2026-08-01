.PHONY: \
	show-coverage \
	build build-server build-worker \
	s3-up \
	db-up db-down db-connect db-erase \
	run-server run-worker \
	test-all test test-race test-integration \
	coverage \
	vet lint ci \
	clean

# файл с переменными окружения для запуска через Makefile
ENV_FILE ?= .env

-include $(ENV_FILE)

# строка подключения приложения к локальному PostgreSQL
DATABASE_DSN ?= postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@localhost:$(POSTGRES_PORT)/$(POSTGRES_DB)?sslmode=disable

export DATABASE_DSN
export S3_ENDPOINT S3_ACCESS_KEY S3_SECRET_KEY S3_BUCKET S3_USE_SSL
export RABBITMQ_URL RABBITMQ_EXCHANGE RABBITMQ_QUEUE
export LOG_LEVEL
export HTTP_ADDRESS MAX_UPLOAD_SIZE SHUTDOWN_TIMEOUT

# команда Docker Compose с выбранным env-файлом
# !!!: для целей db-*, s3-up, run-*, test-integration, coverage и ci
# требуется env-файл с переменными POSTGRES_* и S3_*
#
# NOTE: создать локальный env-файл: `cp .env.example .env`
COMPOSE := docker compose --env-file $(ENV_FILE)

# каталоги для артефактов сборки и пути к бинарникам
BIN_DIR := bin

SERVER := $(BIN_DIR)/server
WORKER := $(BIN_DIR)/worker

# обновить профиль покрытия и вывести общий процент
show-coverage: coverage
	go tool cover -func=coverage.out | tail -n 1

# собрать Сервер и Воркер
build: build-server build-worker

# собрать HTTP-сервер
build-server:
	@mkdir -p $(BIN_DIR)
	go build -o $(SERVER) ./cmd/server

# собрать обработчик фоновых задач
build-worker:
	@mkdir -p $(BIN_DIR)
	go build -o $(WORKER) ./cmd/worker

# создать (при необходимости) и запустить локальный MinIO
# и дождаться его готовности
s3-up:
	$(COMPOSE) up -d --wait minio

# создать (при необходимости) и запустить локальный PostgreSQL
# и дождаться его готовности
db-up:
	$(COMPOSE) up -d --wait postgres

# остановить и удалить контейнер PostgreSQL без удаления данных
db-down:
	$(COMPOSE) down

# подключиться к PostgreSQL через psql
db-connect:
	$(COMPOSE) exec postgres psql -U $(POSTGRES_USER) -d $(POSTGRES_DB)

# удалить контейнер PostgreSQL и локальные данные
db-erase:
	$(COMPOSE) down -v

# собрать и запустить Сервер
run-server: db-up build-server
	$(SERVER)

# собрать и запустить Воркер
run-worker: db-up build-worker
	$(WORKER)

# запустить обычные и интеграционные тесты
test-all: test-race test-integration

# запустить обычные тесты
test:
	go test ./...

# запустить тесты с детектором гонок данных
test-race:
	go test -race ./...

# запустить интеграционные тесты с локальными PostgreSQL и MinIO
test-integration: s3-up db-up
	go test -tags=integration -count=1 ./...

# запустить обычные и интеграционные тесты
# и сохранить атомарный профиль покрытия всего проекта
coverage: s3-up db-up
	go test \
		-count=1 \
		-tags=integration \
		-coverpkg=./... \
		-covermode=atomic \
		-coverprofile=coverage.out \
		./...

# выполнить стандартный статический анализ Go-кода
vet:
	go vet ./...

# проверить проект набором линтеров golangci-lint
lint:
	golangci-lint run ./...

# собрать проект и выполнить полный набор CI-проверок
ci: build test-all vet lint

# удалить артефакты сборки и профиль покрытия
clean:
	rm -rf $(BIN_DIR) coverage.out
