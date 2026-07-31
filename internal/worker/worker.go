// Package worker управляет жизненным циклом Воркера.
package worker

import (
	"context"

	"go.uber.org/zap"
)

// Run запускает Воркер и завершает его при отмене контекста.
func Run(ctx context.Context, lg *zap.Logger) error {
	lg.Info("worker started")
	<-ctx.Done()
	lg.Info("worker stopped")

	return nil
}
