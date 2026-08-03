package handler

import (
	"encoding/json"
	"net/http"
	"time"
)

type avatarUploadResponse struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	URL       string    `json:"url"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type errorResponse struct {
	Error     string `json:"error"`
	Details   string `json:"details,omitempty"`
	MaxSize   int64  `json:"max_size,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, response any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(response)
}

func writeError(
	w http.ResponseWriter,
	r *http.Request,
	status int,
	code string,
	details string,
	maxSize int64,
) {
	writeJSON(w, status, errorResponse{
		Error:     code,
		Details:   details,
		MaxSize:   maxSize,
		RequestID: RequestIDFromContext(r.Context()),
	})
}
