package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/xhrobj/gophprofile/internal/config"
	"github.com/xhrobj/gophprofile/internal/handler"
	"github.com/xhrobj/gophprofile/internal/logger"
	"github.com/xhrobj/gophprofile/internal/server"
)

const (
	// serviceName определяет имя сервиса в структурированных логах
	serviceName = "server"

	// readHeaderTimeout ограничивает время чтения HTTP-заголовков запроса
	readHeaderTimeout = 5 * time.Second

	// idleTimeout ограничивает время простоя keep-alive-соединения
	idleTimeout = 30 * time.Second
)

func main() {
	if err := printBanner(os.Stdout); err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	cfg, err := config.LoadServer()
	if err != nil {
		return fmt.Errorf("load server config: %w", err)
	}

	lg, err := logger.New(serviceName, cfg.LogLevel)
	if err != nil {
		return fmt.Errorf("create server logger: %w", err)
	}
	defer func() {
		_ = lg.Sync()
	}()

	listener, err := net.Listen("tcp", cfg.HTTPAddress)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.HTTPAddress, err)
	}

	errorLog, err := zap.NewStdLogAt(lg.Named("net/http"), zap.ErrorLevel)
	if err != nil {
		return fmt.Errorf("create HTTP error logger: %w", err)
	}

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddress,
		Handler:           handler.NewRouter(lg),
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
		ErrorLog:          errorLog,
	}

	return server.Run(ctx, listener, httpServer, cfg.ShutdownTimeout, lg)
}

func printBanner(output io.Writer) error {
	const banner = `
  ________              .__   __________                _____.__.__
 /  _____/  ____ ______ |  |__\______   \_______  _____/ ____\__|  |   ____
/   \  ___ /  _ \\____ \|  |  \|     ___/\_  __ \/  _ \   __\|  |  | _/ __ \
\    \_\  (  <_> )  |_> >   Y  \    |     |  | \(  <_> )  |  |  |  |_\  ___/
 \______  /\____/|   __/|___|  /____|     |__|   \____/|__|  |__|____/\___  >
        \/       |__|        \/                                           \/
         -= Server: serving every avatar reliably =-

`
	_, err := fmt.Fprint(output, banner)

	return err
}
