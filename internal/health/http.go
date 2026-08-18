package health

import (
	"context"
	"encoding/json"
	"net/http"
)

type reporter interface {
	Check(context.Context) Report
}

// NewLivenessHandler возвращает обработчик, подтверждающий,
// что процесс жив и HTTP-сервер отвечает.
func NewLivenessHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

// NewReadinessHandler возвращает обработчик готовности
// обязательных зависимостей.
func NewReadinessHandler(checker reporter) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		report := checker.Check(r.Context())

		status := http.StatusOK
		if !report.Available() {
			status = http.StatusServiceUnavailable
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(report)
	})
}
