# 👤 [(^.^)] GophProfile

[![Quality gate status](https://sonarcloud.io/api/project_badges/measure?project=xhrobj_gophprofile&metric=alert_status)](https://sonarcloud.io/summary/new_code?id=xhrobj_gophprofile)

[![Quality gate](https://sonarcloud.io/api/project_badges/quality_gate?project=xhrobj_gophprofile)](https://sonarcloud.io/summary/new_code?id=xhrobj_gophprofile)

Сервис для загрузки, обработки и хранения пользовательских аватаров.

Требования к сервису описаны в [SPECIFICATION.md](SPECIFICATION.md).

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
├── cmd/
│   ├── server/    # HTTP-сервер
│   └── worker/    # обработчик фоновых задач
├── docs/          # документация проекта
├── web/           # статические файлы веб-интерфейса
├── SPECIFICATION.md
└── README.md
```

Исходный README шаблона с планируемой структурой и командами сохранён в [`docs/README.template.md`](docs/README.template.md).
