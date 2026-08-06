# 👤 [(^.^)] GophProfile

[![Quality gate status](https://sonarcloud.io/api/project_badges/measure?project=xhrobj_gophprofile&metric=alert_status)](https://sonarcloud.io/summary/new_code?id=xhrobj_gophprofile)

[![Quality gate](https://sonarcloud.io/api/project_badges/quality_gate?project=xhrobj_gophprofile)](https://sonarcloud.io/summary/new_code?id=xhrobj_gophprofile)

GophProfile — сервис для загрузки, хранения, асинхронной обработки и выдачи пользовательских аватаров. Метаданные хранятся в PostgreSQL, оригиналы и миниатюры — в S3-совместимом MinIO, а Server и Worker обмениваются событиями через RabbitMQ.

Требования к сервису описаны в [docs/SPECIFICATION.md](docs/SPECIFICATION.md), HTTP-контракт — в [api/openapi.yml](api/openapi.yml). Исходный README шаблона с планируемой структурой и командами сохранён в [`docs/README.template.md`](docs/README.template.md).

![GophProfile Web UI](docs/images/gophprofile-web-ui.jpg)

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
docker compose --env-file .env up -d --build --wait
```

После запуска доступны:

- Web UI и REST API: <http://localhost:8080>
- healthcheck: <http://localhost:8080/health>
- MinIO Console: <http://localhost:9001>
- RabbitMQ Management: <http://localhost:15672>

Остановить контейнеры без удаления persistent volumes:

```bash
make compose-down
```

Удалить контейнеры вместе с локальными данными PostgreSQL, MinIO и RabbitMQ:

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
| `LOG_LEVEL` | уровень логов: `debug`, `info`, `warn`, `error` | `info` |

При запуске внутри Compose адреса внешних зависимостей переопределяются на внутренние DNS-имена `postgres`, `minio` и `rabbitmq`.

## Основные команды

```text
make build             собрать Server и Worker
make run-server        поднять инфраструктуру, собрать и запустить Server локально
make run-worker        поднять инфраструктуру, собрать и запустить Worker локально
make infra-up          поднять PostgreSQL, MinIO и RabbitMQ
make infra-down        остановить локальную инфраструктуру без удаления данных
make infra-erase       удалить инфраструктуру и persistent volumes
make compose-up        собрать и поднять полный стек Server + Worker + инфраструктура
make compose-down      остановить полный стек без удаления данных
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

## Известные ограничения MVP

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
│   └── SPECIFICATION.md    # ТЗ спринта 11
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
├── tests/e2e/              # black-box E2E happy path
└── web/                    # встроенный frontend
```
