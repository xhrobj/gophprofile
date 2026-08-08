.PHONY: \
	show-coverage \
	build build-server build-worker \
	db-up db-connect \
	s3-up \
	rabbitmq-up \
	infra-up infra-down infra-erase \
	run-server run-worker \
	compose-up compose-down compose-logs \
	test-all test test-race test-integration test-e2e \
	coverage \
	vet lint ci \
	clean

# файл с переменными окружения для запуска через Makefile
# NOTE: создать локальный env-файл: `cp .env.example .env`
ENV_FILE ?= .env

-include $(ENV_FILE)

# строка подключения приложения к локальному PostgreSQL
DATABASE_DSN ?= postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@localhost:$(POSTGRES_PORT)/$(POSTGRES_DB)?sslmode=disable

export DATABASE_DSN
export POSTGRES_DB POSTGRES_PASSWORD POSTGRES_PORT POSTGRES_USER
export S3_ENDPOINT S3_ACCESS_KEY S3_SECRET_KEY S3_BUCKET S3_USE_SSL
export RABBITMQ_USER RABBITMQ_PASSWORD RABBITMQ_URL RABBITMQ_EXCHANGE RABBITMQ_QUEUE
export TRACING_ENABLED OTEL_EXPORTER_OTLP_ENDPOINT
export HTTP_ADDRESS MAX_UPLOAD_SIZE WORKER_METRICS_ADDRESS SHUTDOWN_TIMEOUT
export LOG_LEVEL

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

# создать (при необходимости) и запустить локальный PostgreSQL и дождаться его готовности
db-up:
	docker compose up -d --wait postgres

# подключиться к PostgreSQL через psql
db-connect:
	docker compose exec postgres psql -U $(POSTGRES_USER) -d $(POSTGRES_DB)

# создать (при необходимости) и запустить локальный MinIO и дождаться его готовности
s3-up:
	docker compose up -d --wait minio

# создать (при необходимости) и запустить локальный RabbitMQ и дождаться его готовности
rabbitmq-up:
	docker compose up -d --wait rabbitmq

# поднять внешние зависимости приложения
infra-up: db-up s3-up rabbitmq-up

# остановить внешние зависимости приложения
infra-down:
	docker compose stop postgres minio rabbitmq

# удалить контейнеры, сети и локальные данные Docker Compose
infra-erase:
	docker compose --profile observability down -v

# собрать и запустить Сервер
run-server: infra-up build-server
	docker compose --profile observability up -d --wait jaeger
	$(SERVER)

# собрать и запустить Воркер
run-worker: infra-up build-worker
	docker compose --profile observability up -d --wait jaeger
	$(WORKER)

# собрать и запустить полный локальный стек приложения:
# MinIO (S3), PostgreSQL, RabbitMQ, Сервер, Воркер, Jaeger, Prometheus, Loki, Alloy и Grafana через Docker Compose
compose-up:
	docker compose --profile observability up -d --build --wait

# остановить и удалить контейнеры и сети Docker Compose без удаления данных
compose-down:
	docker compose --profile observability down

# показать логи сервисов Docker Compose
compose-logs:
	docker compose --profile observability logs -f

# запустить обычные и интеграционные тесты
test-all: test-race test-integration

# запустить обычные тесты
test:
	go test ./...

# запустить тесты с детектором гонок данных
test-race:
	go test -race ./...

# запустить интеграционные тесты с локальными PostgreSQL, MinIO и RabbitMQ
test-integration: infra-up
	go test -tags=integration -count=1 ./...

# запустить end-to-end happy path через публичный HTTP API полного Compose-стека
test-e2e: compose-up
	E2E_BASE_URL=http://127.0.0.1:8080 go test -tags=e2e -count=1 ./tests/e2e

# запустить обычные и интеграционные тесты
# и сохранить атомарный профиль покрытия всего проекта
coverage: infra-up
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

# собрать проект и выполнить полный (без e2e) набор CI-проверок
ci: build test-all vet lint

# удалить артефакты сборки и профиль покрытия
clean:
	rm -rf $(BIN_DIR) coverage.out
