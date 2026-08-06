package handler

import (
	"context"
	"net/http"

	"github.com/xhrobj/gophprofile/internal/health"
)

type healthChecker interface {
	Check(context.Context) health.Report
}

func newHealthHandler(checker healthChecker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		report := checker.Check(r.Context())

		status := http.StatusOK
		if !report.Available() {
			status = http.StatusServiceUnavailable
		}

		writeJSON(w, status, report)
	}
}
