# 👤 [(^.^)] GophProfile

[![Quality gate status](https://sonarcloud.io/api/project_badges/measure?project=xhrobj_gophprofile&metric=alert_status)](https://sonarcloud.io/summary/new_code?id=xhrobj_gophprofile)

[![Quality gate](https://sonarcloud.io/api/project_badges/quality_gate?project=xhrobj_gophprofile)](https://sonarcloud.io/summary/new_code?id=xhrobj_gophprofile)

Сервис для загрузки, обработки и хранения пользовательских аватаров.

Требования к сервису описаны в [docs/SPECIFICATION.md](docs/SPECIFICATION.md).

## Запуск

Запуск HTTP-сервера:

```bash
go run ./cmd/server
```

Запуск обработчика фоновых задач:

```bash
go run ./cmd/worker
```

## Структура проекта

```text
.
├── .github/
│   └── workflows/
│       └── go-ci.yaml              # CI для сборки, тестов и статического анализа
├── api/
│   └── openapi.yml                 # OpenAPI-контракт REST API
├── cmd/
│   ├── server/
│   │   └── main.go                 # точка входа Сервера
│   └── worker/
│       └── main.go                 # точка входа Воркера
├── docs/
│   ├── README.template.md          # исходный README шаблона
│   └── SPECIFICATION.md            # техническое задание спринта 11
├── internal/
│   ├── config/                     # конфигурация Сервера и Воркера
│   ├── handler/                    # HTTP-router и middleware
│   ├── logger/                     # структурированное логирование
│   ├── server/                     # lifecycle HTTP-Сервера
│   └── worker/                     # lifecycle Воркера
├── web/
│   └── static/
│       └── index.html              # веб-интерфейс загрузки аватаров
├── .env.example                    # пример переменных окружения
├── LICENSE
├── Makefile
└── README.md
```

Исходный README шаблона с планируемой структурой и командами сохранён в [`docs/README.template.md`](docs/README.template.md).
