// Package web содержит встроенные статические файлы веб-интерфейса GophProfile.
package web

import (
	_ "embed" // подключает поддержку директивы //go:embed
	"io"
	"net/http"
)

//go:embed static/index.html
var indexHTML string

// Handler возвращает HTTP-handler одностраничного веб-интерфейса.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, indexHTML)
	})
}
