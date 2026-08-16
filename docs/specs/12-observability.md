# Спринт 12 — Observability

## Техническое задание на спринт

У вас есть работающий MVP сервиса GophProfile. Он принимает запросы и выполняет бизнес-логику. Но в реальном продакшене этого недостаточно. Если сервис начнёт тормозить или отдавать `500`-е ошибки, без качественных инструментов наблюдаемости вы не поймёте причину.

В этом спринте вы реализуете наблюдаемость: настроите мониторинг (Prometheus) и трейсинг (Jaeger), а для сбора логов сможете выбрать стек — Grafana Loki или OpenSearch/ELK. Выбирайте тот, который вам интереснее освоить.

## Задачи второго этапа разработки GophProfile

### 1. Внедрить инструменты наблюдаемости

Инструментировать код приложения с помощью OpenTelemetry: реализовать распределённый трейсинг (HTTP, БД, S3, брокер), сбор технических и бизнес-метрик, а также настроить структурированное логирование (`slog`) с корреляцией логов и трейсов.

#### 1.1. Трейсинг

- Инструментирование HTTP-запросов
- Трейсы для работы с БД
- Трейсы для S3-операций
- Трейсы для брокера сообщений
- Context propagation между сервисами

```go
import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/trace"
)

func (s *AvatarService) UploadAvatar(ctx context.Context, req *UploadRequest) error {
    ctx, span := otel.Tracer("avatar-service").Start(ctx, "upload_avatar")
    defer span.End()

    span.SetAttributes(
        attribute.String("user_id", req.UserID),
        attribute.String("file_name", req.FileName),
        attribute.Int64("file_size", req.Size),
    )
    // ...
}
```

#### 1.2. Метрики

```go
var (
    uploadsTotal = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "avatars_uploads_total",
            Help: "Total number of avatar uploads",
        },
        []string{"status", "user_id"},
    )

    uploadDuration = promauto.NewHistogramVec(
        prometheus.HistogramOpts{
            Name: "avatars_upload_duration_seconds",
            Help: "Avatar upload duration",
        },
        []string{"status"},
    )

    storageUsage = promauto.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "avatars_storage_bytes",
            Help: "Total storage used by avatars",
        },
        []string{"user_id"},
    )
)
```

#### 1.3. Логирование

- Структурированные логи (JSON)
- Корреляция с trace ID
- Уровни логирования
- Использование `slog`

```go
import "log/slog"

logger := slog.With(
    "service", "avatar-service",
    "trace_id", trace.SpanFromContext(ctx).SpanContext().TraceID(),
)

logger.Info("uploading avatar",
    "user_id", userID,
    "file_size", fileSize,
    "mime_type", mimeType,
)
```

### 2. Развернуть инфраструктуру мониторинга и логирования

Подключить и настроить внешний стек сервисов: Prometheus для сбора метрик, Jaeger для трейсинга, Grafana для визуализации, а также систему сбора логов (Grafana Loki или OpenSearch/ELK).

#### 2.1. Метрики Prometheus

- HTTP-метрики (requests, duration, errors)
- Бизнес-метрики (uploads, storage usage)
- Инфраструктурные метрики (DB connections, queue depth)

#### 2.2. Jaeger

- Distributed tracing
- Performance анализ
- Dependency mapping

#### 2.3. OpenSearch/ELK

- Централизованное логирование
- Алерты на ошибки
- Log aggregation и поиск

### 3. Настроить визуализацию

Создать информативные дашборды в Grafana:

- Service overview
- Request rate, error rate, duration (RED metrics)
- Resource utilization
- Business KPIs

### 4. Бонусная задача: настроить алертинг

Сконфигурируйте правила для Prometheus Alertmanager, чтобы он реагировал на критические показатели: высокий процент ошибок и увеличенное время ответа.

Пример правил:

```yaml
groups:
  - name: avatar-service
    rules:
      - alert: HighErrorRate
        expr: rate(avatars_uploads_total{status="error"}[5m]) / rate(avatars_uploads_total[5m]) > 0.1
        for: 5m
        labels:
          severity: warning

      - alert: HighResponseTime
        expr: histogram_quantile(0.95, avatars_upload_duration_seconds) > 5
        for: 2m
        labels:
          severity: critical
```

---

> Первый спринт был серьёзным вызовом — и вы с ним справились. Впереди не менее увлекательный этап: вы переходите к настройке наблюдаемости. Желаем успехов!

## Критерии приёмки

Перед отправкой кода на ревью проверьте себя по этому чек-листу. Если хотя бы одно условие чек-листа не выполнено, ревьюер вернёт проект на доработку.

### Трейсинг

- Реализована сквозная трассировка (distributed tracing) с полным покрытием компонентов сервиса
- Обеспечена корреляция спанов в рамках единого trace
- Настроен экспорт трейсов в Jaeger, визуализация показывает полную картину запроса

### Метрики

- Prometheus метрики собираются и экспортируются
- Grafana дашборды отображают ключевые метрики
- Business метрики корректно отслеживаются

### Логирование

- Структурированные логи с корреляцией
- ELK/OpenSearch успешно индексирует логи
- Поиск и фильтрация логов работает

### Алертинг (бонусная задача)

- Настроены алерты на критичные метрики
- Алерты срабатывают в тестовых сценариях
