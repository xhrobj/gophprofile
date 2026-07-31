// Package server управляет запуском и корректным завершением HTTP-Сервера.
package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// Run запускает HTTP-Сервер и завершает его при отмене контекста.
func Run(
	ctx context.Context,
	listener net.Listener,
	httpServer *http.Server,
	shutdownTimeout time.Duration,
	lg *zap.Logger,
) error {
	serveErrors := make(chan error, 1)
	go func() {
		serveErrors <- httpServer.Serve(listener)
	}()

	lg.Info("server started", zap.String("address", listener.Addr().String()))

	select {
	case err := <-serveErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	case <-ctx.Done():
	}

	lg.Info("server shutdown started")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	shutdownErr := httpServer.Shutdown(shutdownCtx)
	if shutdownErr != nil {
		lg.Warn(
			"graceful server shutdown failed, forcing close",
			zap.Error(shutdownErr),
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

	lg.Info("server stopped")

	if shutdownErr != nil {
		return fmt.Errorf("shutdown HTTP server: %w", shutdownErr)
	}

	return nil
}
