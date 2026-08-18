.PHONY: \
	show-coverage \
	build build-server build-worker build-migrate build-k8s-images \
	k8s-monitoring-up k8s-monitoring-down \
	helm-lint helm-test helm-check \
	db-up db-connect \
	s3-up \
	rabbitmq-up \
	infra-up infra-down infra-erase \
	run-server run-worker \
	compose-up compose-down compose-logs \
	test-all test test-race test-integration test-e2e test-k8s-e2e \
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
MIGRATE := $(BIN_DIR)/migrate

# локальные Kubernetes images и Docker context Rancher Desktop
K8S_DOCKER_CONTEXT ?= rancher-desktop
K8S_IMAGE_TAG ?= local
K8S_SERVER_IMAGE ?= gophprofile-server:$(K8S_IMAGE_TAG)
K8S_WORKER_IMAGE ?= gophprofile-worker:$(K8S_IMAGE_TAG)
K8S_MIGRATE_IMAGE ?= gophprofile-migrate:$(K8S_IMAGE_TAG)

# Helm Chart приложения
HELM_CHART ?= deploy/helm/gophprofile

# E2E через локальный Kubernetes Ingress
K8S_E2E_BASE_URL ?= http://127.0.0.1
K8S_E2E_HOST ?= gophprofile.local

# Kubernetes monitoring stack
K8S_MONITORING_NAMESPACE ?= monitoring
K8S_MONITORING_RELEASE ?= monitoring
K8S_MONITORING_CHART_VERSION ?= 88.3.0
K8S_MONITORING_VALUES ?= deploy/k8s/monitoring/values.yml
K8S_MONITORING_DASHBOARD ?= deploy/k8s/monitoring/dashboards/kubernetes-overview.json
K8S_MONITORING_DASHBOARD_CONFIGMAP ?= gophprofile-kubernetes-overview

# обновить профиль покрытия и вывести общий процент
show-coverage: coverage
	go tool cover -func=coverage.out | tail -n 1

# собрать Сервер, Воркер и Мигратор
build: build-server build-worker build-migrate

# собрать HTTP-сервер
build-server:
	@mkdir -p $(BIN_DIR)
	go build -o $(SERVER) ./cmd/server

# собрать обработчик фоновых задач
build-worker:
	@mkdir -p $(BIN_DIR)
	go build -o $(WORKER) ./cmd/worker

# собрать Мигратор PostgreSQL
build-migrate:
	@mkdir -p $(BIN_DIR)
	go build -o $(MIGRATE) ./cmd/migrate

# собрать Server, Worker и Migrate images для локального Kubernetes Rancher Desktop
build-k8s-images:
	@context="$$(docker context show)"; \
	if [ "$$context" != "$(K8S_DOCKER_CONTEXT)" ]; then \
		echo "(o_0) Expected Docker context $(K8S_DOCKER_CONTEXT), got $$context" >&2; \
		exit 1; \
	fi
	docker build --target server -t $(K8S_SERVER_IMAGE) .
	docker build --target worker -t $(K8S_WORKER_IMAGE) .
	docker build --target migrate -t $(K8S_MIGRATE_IMAGE) .
	@printf '(*_*) Built Kubernetes images:\n  %s\n  %s\n  %s\n' \
		"$(K8S_SERVER_IMAGE)" "$(K8S_WORKER_IMAGE)" "$(K8S_MIGRATE_IMAGE)"

# проверить Helm Chart статическим линтером
helm-lint:
	helm lint $(HELM_CHART)

# проверить критичные контракты rendered Helm manifests
helm-test:
	go test -tags=helm -count=1 ./tests/helm

# выполнить все локальные проверки Helm Chart
helm-check: helm-lint helm-test

# установить или обновить Kubernetes monitoring stack через Helm
k8s-monitoring-up:
	helm repo add prometheus-community https://prometheus-community.github.io/helm-charts --force-update
	helm upgrade --install $(K8S_MONITORING_RELEASE) prometheus-community/kube-prometheus-stack \
		--version $(K8S_MONITORING_CHART_VERSION) \
		--namespace $(K8S_MONITORING_NAMESPACE) \
		--create-namespace \
		--values $(K8S_MONITORING_VALUES) \
		--wait \
		--timeout 10m
	kubectl create configmap $(K8S_MONITORING_DASHBOARD_CONFIGMAP) \
		--namespace $(K8S_MONITORING_NAMESPACE) \
		--from-file=kubernetes-overview.json=$(K8S_MONITORING_DASHBOARD) \
		--dry-run=client -o yaml | kubectl apply -f -
	kubectl label configmap $(K8S_MONITORING_DASHBOARD_CONFIGMAP) \
		--namespace $(K8S_MONITORING_NAMESPACE) \
		grafana_dashboard=1 --overwrite

# удалить Kubernetes monitoring stack
k8s-monitoring-down:
	kubectl delete configmap $(K8S_MONITORING_DASHBOARD_CONFIGMAP) \
		--namespace $(K8S_MONITORING_NAMESPACE) \
		--ignore-not-found
	helm uninstall $(K8S_MONITORING_RELEASE) --namespace $(K8S_MONITORING_NAMESPACE)

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
run-server: infra-up build-server build-migrate
	docker compose --profile observability up -d --wait jaeger
	$(MIGRATE)
	$(SERVER)

# собрать и запустить Воркер
run-worker: infra-up build-worker build-migrate
	docker compose --profile observability up -d --wait jaeger
	$(MIGRATE)
	$(WORKER)

# собрать и запустить полный локальный стек приложения:
# MinIO (S3), PostgreSQL, RabbitMQ, Сервер, Воркер, Jaeger, Prometheus, Alertmanager, Loki, Alloy и Grafana через Docker Compose
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
# NOTE: если в "тестовом кластере" мало ресурсов, после не забыть выполнить `make compose-down`
test-e2e: compose-up
	E2E_BASE_URL=http://127.0.0.1:8080 go test -tags=e2e -count=1 ./tests/e2e

# запустить end-to-end Happy Path через Traefik Ingress локального Kubernetes
test-k8s-e2e:
	E2E_BASE_URL=$(K8S_E2E_BASE_URL) \
	E2E_HOST=$(K8S_E2E_HOST) \
	go test -tags=e2e -count=1 ./tests/e2e

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
