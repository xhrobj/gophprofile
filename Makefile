.PHONY: \
	show-coverage \
	build build-server build-worker \
	run-server run-worker \
	test test-race \
	coverage \
	vet lint ci \
	clean

# файл с переменными окружения для запуска через Makefile
ENV_FILE ?= .env
ENV_FILE_PATH := $(abspath $(ENV_FILE))

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

# собрать и запустить Сервер
run-server: build-server
	@set -e; set -a; [ ! -f "$(ENV_FILE_PATH)" ] || . "$(ENV_FILE_PATH)"; set +a; exec $(SERVER)

# собрать и запустить Воркер
run-worker: build-worker
	@set -e; set -a; [ ! -f "$(ENV_FILE_PATH)" ] || . "$(ENV_FILE_PATH)"; set +a; exec $(WORKER)

# запустить обычные тесты
test:
	go test ./...

# запустить тесты с детектором гонок данных
test-race:
	go test -race ./...

# запустить тесты и сохранить атомарный профиль покрытия
coverage:
	go test \
		-count=1 \
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
ci: build test-race vet lint

# удалить артефакты сборки и профиль покрытия
clean:
	rm -rf $(BIN_DIR) coverage.out
