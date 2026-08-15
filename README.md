# 👤 [(^.^)] GophProfile

[![(-_-) Go CI](https://github.com/xhrobj/gophprofile/actions/workflows/go-ci.yaml/badge.svg)](https://github.com/xhrobj/gophprofile/actions/workflows/go-ci.yaml)

[![Quality gate status](https://sonarcloud.io/api/project_badges/measure?project=xhrobj_gophprofile&metric=alert_status)](https://sonarcloud.io/summary/new_code?id=xhrobj_gophprofile)

[![Coverage](https://sonarcloud.io/api/project_badges/measure?project=xhrobj_gophprofile&metric=coverage)](https://sonarcloud.io/summary/new_code?id=xhrobj_gophprofile)

GophProfile — сервис для загрузки, хранения, асинхронной обработки и выдачи пользовательских аватаров. Метаданные хранятся в PostgreSQL, оригиналы и миниатюры — в S3-совместимом MinIO, а Server и Worker обмениваются событиями через RabbitMQ.

Требования к сервису описаны в [docs/SPECIFICATION.md](docs/SPECIFICATION.md), HTTP-контракт — в [api/openapi.yml](api/openapi.yml). Исходный README шаблона с планируемой структурой и командами сохранён в [`docs/README.template.md`](docs/README.template.md).

<a href="docs/images/mvp/gophprofile-web-ui.jpg">
  <img src="docs/images/mvp/gophprofile-web-ui-preview.jpg" alt="GophProfile Web UI">
</a>

## Архитектура

```text
Browser / API client
        |
        v
+-------------------+
|      Server       |
|   REST + Web UI   |
+-------------------+
   |       |      |
   |       |      +----------------+
   |       v                       v
   |   PostgreSQL              RabbitMQ
   |   metadata                 events
   |                               |
   v                               v
 MinIO                        +--------+
 originals                    | Worker |
                              +--------+
                               |      |
                               v      v
                           PostgreSQL MinIO
                           statuses   thumbnails
```

Server принимает изображения, сохраняет метаданные в PostgreSQL и оригинал в MinIO, затем публикует `avatar.uploaded`. Worker получает событие, создаёт JPEG-миниатюры `100x100` и `300x300`, сохраняет их в MinIO и обновляет статус обработки. При удалении Server выполняет soft delete в PostgreSQL и публикует `avatar.deleted`, после чего Worker асинхронно удаляет оригинал и миниатюры из MinIO.

Frontend встроен в бинарник Server через `go:embed`, поэтому отдельный web-контейнер не требуется.

## 🚀 Быстрый запуск через Docker Compose

Для полного локального запуска нужны Docker и Docker Compose.

Создайте локальный env-файл:

```bash
cp .env.example .env
```

Соберите и запустите весь стек:

```bash
make compose-up
```

Эквивалентная команда без Make:

```bash
docker compose --env-file .env --profile observability up -d --build --wait
```

После запуска доступны:

- Web UI и REST API: <http://localhost:8080>
- healthcheck: <http://localhost:8080/health>
- Server metrics: <http://localhost:8080/metrics>
- Worker metrics: <http://localhost:9092/metrics>
- Grafana: <http://localhost:3000>
- Prometheus: <http://localhost:9090>
- Alertmanager: <http://localhost:9093>
- Jaeger: <http://localhost:16686>
- MinIO Console: <http://localhost:9001>
- RabbitMQ Management: <http://localhost:15672>

Остановить контейнеры без удаления persistent volumes:

```bash
make compose-down
```

Удалить контейнеры вместе со всеми локальными данными полного Compose-стека:

```bash
make infra-erase
```

## Локальный запуск Go-процессов

Для разработки без контейнеризации Server и Worker нужен Go 1.26. PostgreSQL, MinIO и RabbitMQ при этом по-прежнему можно запускать через Compose.

В первом терминале:

```bash
make run-server
```

Во втором терминале:

```bash
make run-worker
```

`make run-server` и `make run-worker` автоматически поднимают необходимую инфраструктуру и читают `.env`. Другой env-файл можно передать через `ENV_FILE`, например `make run-server ENV_FILE=.env.local`.

## Переменные окружения

| Переменная | Назначение | Значение в `.env.example` |
| --- | --- | --- |
| `HTTP_ADDRESS` | адрес HTTP Server | `:8080` |
| `MAX_UPLOAD_SIZE` | максимальный размер файла в байтах | `10485760` |
| `SHUTDOWN_TIMEOUT` | timeout graceful shutdown Server | `10s` |
| `POSTGRES_DB` | имя локальной PostgreSQL БД для Compose | `gophprofile` |
| `POSTGRES_USER` | пользователь PostgreSQL | `gophprofile` |
| `POSTGRES_PASSWORD` | пароль PostgreSQL | `password` |
| `POSTGRES_PORT` | порт PostgreSQL на host | `5432` |
| `S3_ENDPOINT` | MinIO/S3 endpoint для локальных Go-процессов | `localhost:9000` |
| `S3_ACCESS_KEY` | S3 access key и MinIO root user | `minioadmin` |
| `S3_SECRET_KEY` | S3 secret key и MinIO root password | `minioadmin` |
| `S3_BUCKET` | bucket аватаров | `avatars` |
| `S3_USE_SSL` | использовать TLS для S3 | `false` |
| `RABBITMQ_USER` | пользователь RabbitMQ, создаваемый Compose | `gophprofile` |
| `RABBITMQ_PASSWORD` | пароль RabbitMQ, создаваемый Compose | `password` |
| `RABBITMQ_URL` | RabbitMQ URL для локальных Go-процессов | `amqp://gophprofile:password@localhost:5672/` |
| `RABBITMQ_EXCHANGE` | direct exchange приложения | `avatars.exchange` |
| `RABBITMQ_QUEUE` | очередь Worker | `avatars.processing` |
| `TRACING_ENABLED` | включить экспорт distributed traces | `true` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | базовый OTLP/HTTP endpoint для traces | `http://localhost:4318` |
| `WORKER_METRICS_ADDRESS` | адрес HTTP-сервера метрик Worker | `:9092` |
| `LOG_LEVEL` | уровень логов: `debug`, `info`, `warn`, `error` | `info` |

При запуске внутри Compose адреса зависимостей приложения переопределяются на внутренние DNS-имена `postgres`, `minio` и `rabbitmq`, а OTLP endpoint трассировки — на `jaeger:4318`.

## 👀 Наблюдаемость

GophProfile использует три взаимодополняющих сигнала:

- **metrics** отвечают на вопрос «что происходит с сервисом?» — Prometheus собирает числовые ряды, а Grafana показывает их на дашбордах
- **logs** помогают понять «что произошло в конкретный момент?» — JSON-логи Server и Worker собираются Alloy и хранятся в Loki
- **traces** показывают «где именно прошел запрос и на каком участке возникла задержка или ошибка?» — Server и Worker отправляют OpenTelemetry traces по OTLP/HTTP напрямую в Jaeger

Для централизованного хранения логов выбран **Grafana Loki**: Alloy собирает JSON-логи контейнеров и отправляет их в Loki, а Grafana используется для поиска, корреляции и перехода из логов в трассировку.

### Metrics: RED и бизнес-сигналы

Для HTTP используется модель **RED**: Rate, Errors, Duration. Основные метрики:

- `gophprofile_http_requests_total` — количество HTTP-запросов
- `gophprofile_http_request_duration_seconds` — длительность HTTP-запросов
- `gophprofile_avatar_uploads_total` и `gophprofile_avatar_upload_duration_seconds` — результаты и длительность загрузок
- `gophprofile_worker_processed_events_total` и `gophprofile_worker_processing_duration_seconds` — обработка событий Worker
- `gophprofile_avatar_storage_usage_bytes` — суммарный размер оригиналов
- `gophprofile_postgres_pool_*` — состояние пула PostgreSQL
- RabbitMQ экспортирует метрики очередей через собственный Prometheus endpoint

В Grafana автоматически загружаются два дашборда: **GophProfile / Service Overview** — с HTTP RED и метриками процесса; **GophProfile / Business & Worker** — с метриками загрузок, Worker, хранилища, очередей RabbitMQ и пула PostgreSQL.

<a href="docs/images/observability/grafana-service-overview.png">
  <img src="docs/images/observability/grafana-service-overview-preview.jpg" alt="Grafana Service Overview Dashboard">
</a>

<a href="docs/images/observability/grafana-business-worker.png">
  <img src="docs/images/observability/grafana-business-worker-preview.jpg" alt="Grafana Business & Worker Dashboard">
</a>

### Distributed tracing

Контекст трассировки переносится между Server и Worker в заголовках RabbitMQ-сообщения. Перед публикацией Server добавляет в сообщение W3C Trace Context, а Worker извлекает его перед началом обработки. Поэтому асинхронная обработка продолжается в той же трассе, хотя выполняется другим процессом.

В показанном сценарии один trace включает 6 spans Server и 8 spans Worker — всего 14:

<a href="docs/images/observability/jaeger-upload-search.png">
  <img src="docs/images/observability/jaeger-upload-search-preview.jpg" alt="Jaeger upload traces">
</a>

Внутри конкретной операции виден полный путь `POST /api/v1/avatars`: Server, PostgreSQL, MinIO, публикация события в RabbitMQ и продолжение обработки в Worker.

<a href="docs/images/observability/jaeger-upload-trace.png">
  <img src="docs/images/observability/jaeger-upload-trace-preview.jpg" alt="Jaeger distributed upload trace">
</a>

### Logs → trace correlation

`trace_id` и `span_id` записываются в JSON-логи как обычные поля, а не как Loki labels. Поэтому каждый новый trace не создаёт отдельный поток логов в Loki. При этом записи Server и Worker можно найти по общему `trace_id` и перейти из лога в Jaeger через derived field **View trace**.

<a href="docs/images/observability/loki-view-trace.png">
  <img src="docs/images/observability/loki-view-trace-preview.jpg" alt="Loki View trace">
</a>

Grafana позволяет открыть Loki и Jaeger рядом: слева — связанные логи приложения, справа — соответствующая трасса.

<a href="docs/images/observability/loki-jaeger-correlation.png">
  <img src="docs/images/observability/loki-jaeger-correlation-preview.jpg" alt="Loki and Jaeger correlation">
</a>

Типовой путь диагностики:

1. В Grafana заметить рост error rate или p95 latency
2. В Loki отфильтровать JSON-логи по `service`, уровню или `trace_id`
3. Из лога перейти в Jaeger по `trace_id` и посмотреть весь путь операции

### ⚡ Alerting

Prometheus отправляет сработавшие алерты в Alertmanager. В репозитории настроены правила `HighErrorRate` и `HighResponseTime`; отправка уведомлений во внешние системы намеренно не настроена.

`HighErrorRate` переходит в `FIRING`, если доля `5xx`-ответов Server за последние пять минут превышает 10% и это состояние сохраняется ещё пять минут:

<a href="docs/images/observability/prometheus-high-error-rate-firing.jpg">
  <img src="docs/images/observability/prometheus-high-error-rate-firing-preview.jpg" alt="Prometheus HighErrorRate firing">
</a>

После перехода правила в `FIRING` активный alert появляется в Alertmanager с `alertname="HighErrorRate"` и `severity="warning"`:

<a href="docs/images/observability/alertmanager-high-error-rate.jpg">
  <img src="docs/images/observability/alertmanager-high-error-rate-preview.jpg" alt="Alertmanager HighErrorRate">
</a>

## Kubernetes

Kubernetes deployment GophProfile упакован в Helm Chart `deploy/helm/gophprofile`. Chart разворачивает Server, Worker, PostgreSQL, MinIO, RabbitMQ, migration Job, HPA, Traefik Ingress, NetworkPolicy, ServiceMonitor и PrometheusRule. `kube-prometheus-stack` остаётся инфраструктурой кластера и устанавливается отдельно.

Для локального Rancher Desktop сначала подготовьте Secret-файлы из `.example` и заполните их локальными значениями:

```bash
cp deploy/k8s/app/secret.yml.example deploy/k8s/app/secret.yml
cp deploy/k8s/postgres/secret.yml.example deploy/k8s/postgres/secret.yml
cp deploy/k8s/minio/secret.yml.example deploy/k8s/minio/secret.yml
cp deploy/k8s/rabbitmq/secret.yml.example deploy/k8s/rabbitmq/secret.yml
```

Реальные Secret-файлы исключены из Git. Monitoring stack нужен до установки application Chart, потому что Chart создает `ServiceMonitor` и `PrometheusRule`:

```bash
make k8s-monitoring-up
make helm-check
make build-k8s-images
```

Namespace и внешние Secrets создаются отдельно от Helm release:

```bash
kubectl create namespace gophprofile --dry-run=client -o yaml | kubectl apply -f -

kubectl apply -n gophprofile \
  -f deploy/k8s/app/secret.yml \
  -f deploy/k8s/postgres/secret.yml \
  -f deploy/k8s/minio/secret.yml \
  -f deploy/k8s/rabbitmq/secret.yml
```

Локальные параметры образов и Rancher Desktop собраны в `values-local.yaml`. Release устанавливается или обновляется обычной командой Helm:

```bash
helm upgrade --install gophprofile deploy/helm/gophprofile \
  --namespace gophprofile \
  -f deploy/helm/gophprofile/values-local.yaml \
  --timeout 5m

kubectl rollout status deployment/server -n gophprofile --timeout=120s
kubectl rollout status deployment/worker -n gophprofile --timeout=120s
```

При первой установке migration Job запускается как `post-install` hook и ждёт доступности PostgreSQL; init containers Server и Worker не пропускают workloads дальше старта, пока `schema_migrations` не зафиксирует успешно завершённую миграцию. При upgrade тот же Job выполняется как `pre-upgrade` hook до обновления workloads. Поэтому для первой установки Helm не запускается с `--wait`: иначе ожидание Ready application Pods происходило бы раньше `post-install` hook.

Статус release можно посмотреть командой `helm status gophprofile -n gophprofile`, удалить release — `helm uninstall gophprofile -n gophprofile`. Успешный migration hook удаляется Helm автоматически; при ошибке Job остаётся для диагностики. Локальные Secrets и persistent PVC в lifecycle Helm release не входят и после uninstall сохраняются.

После перехода на Chart Helm templates являются единственным источником application Kubernetes manifests. В `deploy/k8s/` остаются только bootstrap-файлы вне application release: Namespace, `.example` для локальных Secrets и `monitoring/values.yml` для внешнего `kube-prometheus-stack`.

Внешний HTTP-трафик Server проходит через Traefik Ingress. На Ingress настроены ограничение размера upload и rate limiting: 20 запросов в секунду с burst до 40. Внутренние metrics и health checks через публичный Ingress не маршрутизируются.

## Основные команды

```text
make build             собрать Server, Worker и Migrator
make run-server        поднять инфраструктуру, собрать и запустить Server локально
make run-worker        поднять инфраструктуру, собрать и запустить Worker локально
make infra-up          поднять PostgreSQL, MinIO и RabbitMQ
make infra-down        остановить локальную инфраструктуру без удаления данных
make infra-erase       удалить инфраструктуру и persistent volumes
make compose-up        собрать и поднять полный стек Server + Worker + инфраструктура
make compose-down      остановить полный стек без удаления данных
make build-k8s-images  собрать локальные Kubernetes images
make k8s-monitoring-up установить/обновить kube-prometheus-stack
make helm-check        проверить Helm Chart: lint + semantic render tests
make test              запустить обычные тесты
make test-race         запустить обычные тесты с race detector
make test-integration  запустить integration-тесты с реальной инфраструктурой
make test-e2e          поднять полный Compose-стек и выполнить HTTP E2E happy path
make coverage          построить coverage.out с integration-тестами
make show-coverage     вывести итоговый процент покрытия
make vet               запустить go vet
make lint              запустить golangci-lint
make ci                build + race + integration + vet + lint
make clean             удалить локальные бинарники и coverage.out
```

## REST API

Все URL ниже относятся к Server на `http://localhost:8080`.

| Метод | Endpoint | Назначение |
| --- | --- | --- |
| `POST` | `/api/v1/avatars` | загрузить аватар; обязательны `X-User-ID` и multipart-поле `file` |
| `GET` | `/api/v1/avatars/{avatar_id}` | получить оригинал или миниатюру через `?size=original | 100x100 | 300x300` |
| `GET` | `/api/v1/avatars/{avatar_id}/metadata` | получить метаданные и состояние обработки |
| `DELETE` | `/api/v1/avatars/{avatar_id}` | удалить аватар владельца; обязателен `X-User-ID` |
| `GET` | `/api/v1/users/{user_id}/avatar` | получить текущий аватар пользователя |
| `DELETE` | `/api/v1/users/{user_id}/avatar` | удалить текущий аватар пользователя; обязателен `X-User-ID` |
| `GET` | `/api/v1/users/{user_id}/avatars` | получить список аватаров пользователя |
| `GET` | `/health` | проверить PostgreSQL, MinIO и RabbitMQ |

Поддерживаются JPEG, PNG и WebP размером до 10 MiB. Миниатюры создаются асинхронно, поэтому сразу после `201 Created` их получение может временно возвращать `404`; готовность отражается в `processing_status` metadata.

Ошибки, сформированные Server, возвращаются в едином JSON-формате с `error`, безопасным `details` и `request_id`:

- `400 Bad Request` — некорректный запрос
- `404 Not Found` — ресурс не найден
- `413 Content Too Large` — превышен допустимый размер upload
- `500 Internal Server Error` — неожиданная внутренняя ошибка
- `503 Service Unavailable` — временно недоступны PostgreSQL, S3 или RabbitMQ

Технические детали внутренних и dependency errors остаются в структурированных логах и не передаются клиенту. `429 Too Many Requests` при превышении ingress rate limit формирует Traefik до передачи запроса Server.

Пример загрузки:

```bash
curl -i \
  -H 'X-User-ID: Bob' \
  -F 'file=@avatar.png' \
  http://localhost:8080/api/v1/avatars
```

Получение metadata:

```bash
curl http://localhost:8080/api/v1/avatars/<avatar_id>/metadata
```

Получение миниатюры:

```bash
curl -o avatar-100.jpg 'http://localhost:8080/api/v1/avatars/<avatar_id>?size=100x100'
```

Удаление:

```bash
curl -i \
  -X DELETE \
  -H 'X-User-ID: Bob' \
  http://localhost:8080/api/v1/avatars/<avatar_id>
```

## Web UI

Server отдаёт встроенный интерфейс по маршрутам:

- `/` — стартовая страница;
- `/web/upload` — загрузка аватара;
- `/web/gallery/{user_id}` — галерея пользователя.

Web UI использует тот же REST API и тот же origin, поэтому отдельная CORS-конфигурация для локального сценария не нужна.

## Тестирование

Обычные unit-тесты не требуют внешней инфраструктуры:

```bash
make test
```

Integration-тесты работают с реальными PostgreSQL, MinIO и RabbitMQ и автоматически поднимают их через Compose:

```bash
make test-integration
```

E2E-тест проверяет критический пользовательский поток только через публичный HTTP API: загрузку PNG, ожидание завершения Worker, получение оригинала и двух миниатюр, metadata, удаление и последующие `404`/пустой список.

```bash
make test-e2e
```

Для полного локального набора проверок перед коммитом:

```bash
make ci
make test-e2e
```

## Хранение и обработка

Канонические S3 keys:

```text
originals/{user_id}/{avatar_id}/{file_name}
thumbnails/{user_id}/{avatar_id}/100x100.jpg
thumbnails/{user_id}/{avatar_id}/300x300.jpg
```

RabbitMQ использует durable direct exchange и очередь с DLQ. Worker работает с manual ack, `prefetch=1`, bounded retry и идемпотентными переходами состояния; повторная доставка уже обработанного события не должна повторять побочные эффекты.

Удаление выполняется в два шага: запись сразу скрывается через soft delete в PostgreSQL, а очистка объектов MinIO выполняется Worker после события `avatar.deleted`.

## Ограничения MVP

- `X-User-ID` является идентификатором владельца для учебного MVP и не заменяет реальную аутентификацию или авторизацию.
- Между PostgreSQL и RabbitMQ не используется transactional outbox: сбои публикации событий обрабатываются recovery-логикой, поэтому атомарной гарантии между изменением данных и публикацией сообщения нет. Операции с S3 также компенсируются на уровне приложения.
- Миниатюры создаются в размерах `100x100` и `300x300` и всегда сохраняются как JPEG; динамическое преобразование формата не реализовано.

## Структура проекта

```text
.
├── api/                    # OpenAPI-контракт
├── cmd/
│   ├── server/             # composition root HTTP Server
│   └── worker/             # composition root Worker
├── docs/
│   └── SPECIFICATION.md    # входная точка к ТЗ спринтов
├── internal/
│   ├── broker/rabbitmq/    # RabbitMQ adapter и topology
│   ├── config/             # environment configuration
│   ├── event/              # события avatar.uploaded/avatar.deleted
│   ├── handler/            # HTTP handlers и middleware
│   ├── health/             # aggregate healthcheck
│   ├── imageprocessor/     # создание миниатюр
│   ├── logger/             # slog logging
│   ├── migration/          # автоматическое применение миграций
│   ├── model/              # доменная модель
│   ├── postgres/           # PostgreSQL repository
│   ├── s3/                 # S3/MinIO adapter
│   ├── server/             # lifecycle Server
│   ├── service/            # application services
│   └── worker/             # обработка RabbitMQ-событий
├── migrations/             # встроенные SQL-миграции
├── observability/          # Prometheus, Alertmanager, Grafana, Loki и Alloy
├── tests/e2e/              # black-box E2E happy path
└── web/                    # встроенный frontend
```
