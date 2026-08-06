package health

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakePinger struct {
	err   error
	calls int
	ping  func(context.Context) error
}

var errDependencyUnavailable = errors.New("dependency unavailable")

func TestChecker_Check(t *testing.T) {
	tests := []struct {
		name         string
		databaseErr  error
		s3Err        error
		brokerErr    error
		wantStatus   string
		wantDatabase Component
		wantS3       Component
		wantBroker   Component
	}{
		{
			name:         "all dependencies are available",
			wantStatus:   statusOK,
			wantDatabase: Component{Status: statusOK},
			wantS3:       Component{Status: statusOK},
			wantBroker:   Component{Status: statusOK},
		},
		{
			name:         "database is unavailable",
			databaseErr:  errDependencyUnavailable,
			wantStatus:   statusUnavailable,
			wantDatabase: Component{Status: componentStatusError, Error: errDependencyUnavailable.Error()},
			wantS3:       Component{Status: statusOK},
			wantBroker:   Component{Status: statusOK},
		},
		{
			name:         "S3 is unavailable",
			s3Err:        errDependencyUnavailable,
			wantStatus:   statusUnavailable,
			wantDatabase: Component{Status: statusOK},
			wantS3:       Component{Status: componentStatusError, Error: errDependencyUnavailable.Error()},
			wantBroker:   Component{Status: statusOK},
		},
		{
			name:         "broker is unavailable",
			brokerErr:    errDependencyUnavailable,
			wantStatus:   statusUnavailable,
			wantDatabase: Component{Status: statusOK},
			wantS3:       Component{Status: statusOK},
			wantBroker:   Component{Status: componentStatusError, Error: errDependencyUnavailable.Error()},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			database := &fakePinger{err: tt.databaseErr}
			s3 := &fakePinger{err: tt.s3Err}
			broker := &fakePinger{err: tt.brokerErr}
			checker := NewChecker(database, s3, broker)

			report := checker.Check(context.Background())

			if report.Status != tt.wantStatus {
				t.Errorf("Check() status = %q, want %q", report.Status, tt.wantStatus)
			}
			if report.Components.Database != tt.wantDatabase {
				t.Errorf("Check() database = %+v, want %+v", report.Components.Database, tt.wantDatabase)
			}
			if report.Components.S3 != tt.wantS3 {
				t.Errorf("Check() S3 = %+v, want %+v", report.Components.S3, tt.wantS3)
			}
			if report.Components.Broker != tt.wantBroker {
				t.Errorf("Check() broker = %+v, want %+v", report.Components.Broker, tt.wantBroker)
			}
			if database.calls != 1 || s3.calls != 1 || broker.calls != 1 {
				t.Errorf(
					"Ping() calls = database:%d S3:%d broker:%d, want 1/1/1",
					database.calls,
					s3.calls,
					broker.calls,
				)
			}
		})
	}
}

func TestChecker_Check_UsesIndependentTimeouts(t *testing.T) {
	const timeout = 10 * time.Millisecond

	database := &fakePinger{ping: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	s3 := &fakePinger{ping: assertActiveDeadline(t)}
	broker := &fakePinger{ping: assertActiveDeadline(t)}
	checker := &Checker{
		database: database,
		s3:       s3,
		broker:   broker,
		timeout:  timeout,
	}

	report := checker.Check(context.Background())

	if report.Status != statusUnavailable {
		t.Errorf("Check() status = %q, want %q", report.Status, statusUnavailable)
	}
	if report.Components.Database.Error != context.DeadlineExceeded.Error() {
		t.Errorf(
			"Check() database error = %q, want %q",
			report.Components.Database.Error,
			context.DeadlineExceeded,
		)
	}
	if s3.calls != 1 || broker.calls != 1 {
		t.Errorf("Ping() calls after database timeout = S3:%d broker:%d, want 1/1", s3.calls, broker.calls)
	}
}

func (f *fakePinger) Ping(ctx context.Context) error {
	f.calls++
	if f.ping != nil {
		return f.ping(ctx)
	}

	return f.err
}

func assertActiveDeadline(t *testing.T) func(context.Context) error {
	t.Helper()

	return func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			t.Errorf("Ping() context error = %v, want active context", err)
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Error("Ping() context has no deadline")
		}

		return nil
	}
}
