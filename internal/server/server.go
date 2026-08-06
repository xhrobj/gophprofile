// Package server управляет запуском и корректным завершением HTTP-Сервера.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// Run запускает HTTP-Сервер и завершает его при отмене контекста.
func Run(
	ctx context.Context,
	listener net.Listener,
	httpServer *http.Server,
	shutdownTimeout time.Duration,
	lg *slog.Logger,
) error {
	serveErrors := make(chan error, 1)
	go func() {
		serveErrors <- httpServer.Serve(listener)
	}()

	lg.InfoContext(ctx, "server started", slog.String("address", listener.Addr().String()))

	select {
	case err := <-serveErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	case <-ctx.Done():
	}

	lg.InfoContext(ctx, "server shutdown started")

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()

	shutdownErr := httpServer.Shutdown(shutdownCtx)
	if shutdownErr != nil {
		lg.WarnContext(ctx,
			"graceful server shutdown failed, forcing close",
			slog.Any("error", shutdownErr),
		)

		if closeErr := httpServer.Close(); closeErr != nil {
			return errors.Join(
				fmt.Errorf("shutdown HTTP server: %w", shutdownErr),
				fmt.Errorf("close HTTP server: %w", closeErr),
			)
		}
	}

	if err := <-serveErrors; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve HTTP during shutdown: %w", err)
	}

	lg.InfoContext(ctx, "server stopped")

	if shutdownErr != nil {
		return fmt.Errorf("shutdown HTTP server: %w", shutdownErr)
	}

	return nil
}
