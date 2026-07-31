package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/xhrobj/gophprofile/internal/config"
	"github.com/xhrobj/gophprofile/internal/logger"
	"github.com/xhrobj/gophprofile/internal/worker"
)

const serviceName = "worker"

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
	cfg, err := config.LoadWorker()
	if err != nil {
		return fmt.Errorf("load worker config: %w", err)
	}

	lg, err := logger.New(serviceName, cfg.LogLevel)
	if err != nil {
		return fmt.Errorf("create worker logger: %w", err)
	}
	defer func() {
		_ = lg.Sync()
	}()

	return worker.Run(ctx, lg)
}

func printBanner(output io.Writer) error {
	const banner = `
  ________              .__   __________                _____.__.__
 /  _____/  ____ ______ |  |__\______   \_______  _____/ ____\__|  |   ____
/   \  ___ /  _ \\____ \|  |  \|     ___/\_  __ \/  _ \   __\|  |  | _/ __ \
\    \_\  (  <_> )  |_> >   Y  \    |     |  | \(  <_> )  |  |  |  |_\  ___/
 \______  /\____/|   __/|___|  /____|     |__|   \____/|__|  |__|____/\___  >
        \/       |__|        \/                                           \/
         -= Worker: processing every avatar with care =-

`
	_, err := fmt.Fprint(output, banner)

	return err
}
