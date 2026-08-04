package worker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/xhrobj/gophprofile/internal/broker"
	"github.com/xhrobj/gophprofile/internal/event"
	"github.com/xhrobj/gophprofile/internal/imageprocessor"
	"github.com/xhrobj/gophprofile/internal/model"
	"github.com/xhrobj/gophprofile/internal/s3"
)

const (
	testAvatarID  = "c0decafe-babe-4bed-b042-feeddeadbeef"
	testMessageID = "c0decafe-babe-4bed-b043-feeddeadbeef"
)

type fakeConsumer struct{}

type fakeDelivery struct {
	body       []byte
	messageID  string
	routingKey string
	ackCalls   int
	nackCalls  []bool
}

type claimResult struct {
	claimed bool
	err     error
}

type fakeRepository struct {
	claimResults       []claimResult
	claimCalls         int
	completeCalls      int
	completedKeys      map[model.ThumbnailSize]string
	completeErr        error
	statusUpdates      []model.ProcessingStatus
	updateStatusErrors []error
	updateStatusFunc   func(context.Context, string, model.ProcessingStatus) error
}

type putCall struct {
	key         string
	content     []byte
	contentType string
}

type fakeStorage struct {
	content      []byte
	getErrors    []error
	getFunc      func(context.Context, string) (io.ReadCloser, error)
	getCalls     int
	getKeys      []string
	puts         []putCall
	putErr       error
	deletedKeys  []string
	deleteCalls  int
	deleteErrors []error
	deleteFunc   func(context.Context, ...string) error
}

type fakeImageProcessor struct {
	thumbnails []imageprocessor.Thumbnail
	err        error
	calls      int
}

func TestWorker_Run_StopsOnContextCancellation(t *testing.T) {
	var output bytes.Buffer
	lg := testLogger(&output)
	consumer := &fakeConsumer{}
	avatarWorker := New(consumer, nil, nil, nil, lg)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		done <- avatarWorker.Run(ctx)
	}()

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Worker.Run() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Worker.Run() did not stop after context cancellation")
	}

	messages := logMessages(t, &output)
	want := []string{"worker started", "worker stopped"}
	if !reflect.DeepEqual(messages, want) {
		t.Errorf("log messages = %v, want %v", messages, want)
	}
}

func TestWorker_HandleDelivery_ProcessesAvatar(t *testing.T) {
	repository := &fakeRepository{claimResults: []claimResult{{claimed: true}}}
	storage := &fakeStorage{content: []byte("original")}
	processor := &fakeImageProcessor{thumbnails: testThumbnails()}
	message := testEvent()
	item := newFakeUploadedDelivery(t, message)
	avatarWorker := newTestWorker(repository, storage, processor)

	if err := avatarWorker.handleDelivery(context.Background(), item); err != nil {
		t.Fatalf("handleDelivery() error = %v", err)
	}

	if item.ackCalls != 1 || len(item.nackCalls) != 0 {
		t.Fatalf("delivery ack/nack = %d/%v, want 1/[]", item.ackCalls, item.nackCalls)
	}
	if repository.claimCalls != 1 {
		t.Errorf("ClaimForProcessing() calls = %d, want 1", repository.claimCalls)
	}
	if storage.getCalls != 1 || storage.getKeys[0] != message.S3Key {
		t.Errorf("Storage.Get() calls/keys = %d/%v, want 1/%q", storage.getCalls, storage.getKeys, message.S3Key)
	}
	if processor.calls != 1 {
		t.Errorf("ImageProcessor.Process() calls = %d, want 1", processor.calls)
	}

	wantKeys := map[model.ThumbnailSize]string{
		model.ThumbnailSize100x100: s3.ThumbnailKey("Alice", testAvatarID, model.ThumbnailSize100x100),
		model.ThumbnailSize300x300: s3.ThumbnailKey("Alice", testAvatarID, model.ThumbnailSize300x300),
	}
	if !reflect.DeepEqual(repository.completedKeys, wantKeys) {
		t.Errorf("CompleteProcessing() keys = %#v, want %#v", repository.completedKeys, wantKeys)
	}
	if len(storage.puts) != 2 {
		t.Fatalf("Storage.Put() calls = %d, want 2", len(storage.puts))
	}
	for _, put := range storage.puts {
		if put.contentType != "image/jpeg" {
			t.Errorf("Storage.Put() content type = %q, want image/jpeg", put.contentType)
		}
	}
}

func TestWorker_HandleDelivery_AcknowledgesDuplicate(t *testing.T) {
	repository := &fakeRepository{claimResults: []claimResult{{claimed: false}}}
	storage := &fakeStorage{}
	processor := &fakeImageProcessor{}
	item := newFakeUploadedDelivery(t, testEvent())
	avatarWorker := newTestWorker(repository, storage, processor)

	if err := avatarWorker.handleDelivery(context.Background(), item); err != nil {
		t.Fatalf("handleDelivery() error = %v", err)
	}

	if item.ackCalls != 1 || len(item.nackCalls) != 0 {
		t.Fatalf("delivery ack/nack = %d/%v, want 1/[]", item.ackCalls, item.nackCalls)
	}
	if storage.getCalls != 0 || processor.calls != 0 || repository.completeCalls != 0 {
		t.Errorf(
			"duplicate side effects: get=%d process=%d complete=%d, want all zero",
			storage.getCalls,
			processor.calls,
			repository.completeCalls,
		)
	}
}

func TestWorker_HandleDelivery_RetriesClaim(t *testing.T) {
	errPostgreSQLUnavailable := errors.New("PostgreSQL unavailable")
	repository := &fakeRepository{
		claimResults: []claimResult{
			{err: errPostgreSQLUnavailable},
			{err: errPostgreSQLUnavailable},
			{claimed: true},
		},
	}
	storage := &fakeStorage{content: []byte("original")}
	processor := &fakeImageProcessor{thumbnails: testThumbnails()}
	item := newFakeUploadedDelivery(t, testEvent())
	avatarWorker := newTestWorker(repository, storage, processor)

	if err := avatarWorker.handleDelivery(context.Background(), item); err != nil {
		t.Fatalf("handleDelivery() error = %v", err)
	}

	if repository.claimCalls != maxRetryAttempts {
		t.Errorf("ClaimForProcessing() calls = %d, want %d", repository.claimCalls, maxRetryAttempts)
	}
	if item.ackCalls != 1 || len(item.nackCalls) != 0 {
		t.Errorf("delivery ack/nack = %d/%v, want 1/[]", item.ackCalls, item.nackCalls)
	}
	if storage.getCalls != 1 || processor.calls != 1 || repository.completeCalls != 1 {
		t.Errorf(
			"processing side effects: get=%d process=%d complete=%d, want 1/1/1",
			storage.getCalls,
			processor.calls,
			repository.completeCalls,
		)
	}
}

func TestWorker_HandleDelivery_DeadLettersWhenClaimRetriesExhausted(t *testing.T) {
	errPostgreSQLUnavailable := errors.New("PostgreSQL unavailable")
	repository := &fakeRepository{
		claimResults: []claimResult{
			{err: errPostgreSQLUnavailable},
			{err: errPostgreSQLUnavailable},
			{err: errPostgreSQLUnavailable},
		},
	}
	storage := &fakeStorage{}
	processor := &fakeImageProcessor{}
	item := newFakeUploadedDelivery(t, testEvent())
	avatarWorker := newTestWorker(repository, storage, processor)

	if err := avatarWorker.handleDelivery(context.Background(), item); err != nil {
		t.Fatalf("handleDelivery() error = %v", err)
	}

	if repository.claimCalls != maxRetryAttempts {
		t.Errorf("ClaimForProcessing() calls = %d, want %d", repository.claimCalls, maxRetryAttempts)
	}
	if item.ackCalls != 0 || !reflect.DeepEqual(item.nackCalls, []bool{false}) {
		t.Fatalf("delivery ack/nack = %d/%v, want 0/[false]", item.ackCalls, item.nackCalls)
	}
	if storage.getCalls != 0 || processor.calls != 0 || repository.completeCalls != 0 {
		t.Errorf(
			"unclaimed message side effects: get=%d process=%d complete=%d, want all zero",
			storage.getCalls,
			processor.calls,
			repository.completeCalls,
		)
	}
	if len(repository.statusUpdates) != 0 {
		t.Errorf("processing status updates = %v, want none", repository.statusUpdates)
	}
}

func TestWorker_HandleDelivery_RetriesTransientFailure(t *testing.T) {
	repository := &fakeRepository{claimResults: []claimResult{{claimed: true}}}
	storage := &fakeStorage{
		content: []byte("original"),
		getErrors: []error{
			errors.New("temporary S3 error"),
			errors.New("temporary S3 error"),
			nil,
		},
	}
	processor := &fakeImageProcessor{thumbnails: testThumbnails()}
	item := newFakeUploadedDelivery(t, testEvent())
	avatarWorker := newTestWorker(repository, storage, processor)

	if err := avatarWorker.handleDelivery(context.Background(), item); err != nil {
		t.Fatalf("handleDelivery() error = %v", err)
	}

	if storage.getCalls != maxRetryAttempts {
		t.Errorf("Storage.Get() calls = %d, want %d", storage.getCalls, maxRetryAttempts)
	}
	if item.ackCalls != 1 || len(item.nackCalls) != 0 {
		t.Errorf("delivery ack/nack = %d/%v, want 1/[]", item.ackCalls, item.nackCalls)
	}
	if len(repository.statusUpdates) != 0 {
		t.Errorf("processing status updates = %v, want none", repository.statusUpdates)
	}
}

func TestWorker_HandleDelivery_DeadLettersAfterRetries(t *testing.T) {
	repository := &fakeRepository{claimResults: []claimResult{{claimed: true}}}
	storage := &fakeStorage{
		getErrors: []error{
			errors.New("S3 unavailable"),
			errors.New("S3 unavailable"),
			errors.New("S3 unavailable"),
		},
	}
	processor := &fakeImageProcessor{}
	item := newFakeUploadedDelivery(t, testEvent())
	avatarWorker := newTestWorker(repository, storage, processor)

	if err := avatarWorker.handleDelivery(context.Background(), item); err != nil {
		t.Fatalf("handleDelivery() error = %v", err)
	}

	if item.ackCalls != 0 || !reflect.DeepEqual(item.nackCalls, []bool{false}) {
		t.Fatalf("delivery ack/nack = %d/%v, want 0/[false]", item.ackCalls, item.nackCalls)
	}
	if storage.getCalls != maxRetryAttempts {
		t.Errorf("Storage.Get() calls = %d, want %d", storage.getCalls, maxRetryAttempts)
	}
	if !reflect.DeepEqual(repository.statusUpdates, []model.ProcessingStatus{model.ProcessingStatusFailed}) {
		t.Errorf("processing status updates = %v, want [failed]", repository.statusUpdates)
	}

	wantDeleted := []string{
		s3.ThumbnailKey("Alice", testAvatarID, model.ThumbnailSize100x100),
		s3.ThumbnailKey("Alice", testAvatarID, model.ThumbnailSize300x300),
	}
	if !reflect.DeepEqual(storage.deletedKeys, wantDeleted) {
		t.Errorf("Storage.Delete() keys = %v, want %v", storage.deletedKeys, wantDeleted)
	}
}

func TestWorker_FinalizeFailure_UsesSeparateRecoveryContexts(t *testing.T) {
	var statusCtx context.Context
	var cleanupCtx context.Context
	var calls []string

	repository := &fakeRepository{
		updateStatusFunc: func(ctx context.Context, _ string, _ model.ProcessingStatus) error {
			statusCtx = ctx
			calls = append(calls, "status")

			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("UpdateProcessingStatus() context has no deadline")
			}

			return nil
		},
	}
	storage := &fakeStorage{
		deleteFunc: func(ctx context.Context, _ ...string) error {
			cleanupCtx = ctx
			calls = append(calls, "cleanup")

			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("Storage.Delete() context has no deadline")
			}

			return nil
		},
	}
	avatarWorker := newTestWorker(repository, storage, &fakeImageProcessor{})

	avatarWorker.finalizeFailure(zap.NewNop(), testEvent())

	if !reflect.DeepEqual(calls, []string{"status", "cleanup"}) {
		t.Errorf("recovery calls = %v, want [status cleanup]", calls)
	}
	if statusCtx == cleanupCtx {
		t.Error("recovery operations share the same context, want separate contexts")
	}
	if !reflect.DeepEqual(repository.statusUpdates, []model.ProcessingStatus{model.ProcessingStatusFailed}) {
		t.Errorf("processing status updates = %v, want [failed]", repository.statusUpdates)
	}
	if storage.deleteCalls != 1 {
		t.Fatalf("Storage.Delete() calls = %d, want 1", storage.deleteCalls)
	}
}

func TestWorker_HandleDelivery_DeadLettersWhenFailedStatusUpdateFails(t *testing.T) {
	repository := &fakeRepository{
		claimResults: []claimResult{{claimed: true}},
		updateStatusErrors: []error{
			errors.New("PostgreSQL unavailable"),
			errors.New("PostgreSQL unavailable"),
			errors.New("PostgreSQL unavailable"),
		},
	}
	storage := &fakeStorage{
		getErrors: []error{
			errors.New("S3 unavailable"),
			errors.New("S3 unavailable"),
			errors.New("S3 unavailable"),
		},
	}
	item := newFakeUploadedDelivery(t, testEvent())
	avatarWorker := newTestWorker(repository, storage, &fakeImageProcessor{})

	if err := avatarWorker.handleDelivery(context.Background(), item); err != nil {
		t.Fatalf("handleDelivery() error = %v", err)
	}

	wantStatuses := []model.ProcessingStatus{
		model.ProcessingStatusFailed,
		model.ProcessingStatusFailed,
		model.ProcessingStatusFailed,
	}
	if !reflect.DeepEqual(repository.statusUpdates, wantStatuses) {
		t.Errorf("processing status updates = %v, want %v", repository.statusUpdates, wantStatuses)
	}
	if !reflect.DeepEqual(item.nackCalls, []bool{false}) {
		t.Errorf("delivery nack calls = %v, want [false]", item.nackCalls)
	}
}

func TestWorker_HandleDelivery_DoesNotRetryInvalidImage(t *testing.T) {
	repository := &fakeRepository{claimResults: []claimResult{{claimed: true}}}
	storage := &fakeStorage{content: []byte("broken image")}
	processor := &fakeImageProcessor{err: imageprocessor.ErrInvalidImage}
	item := newFakeUploadedDelivery(t, testEvent())
	avatarWorker := newTestWorker(repository, storage, processor)

	if err := avatarWorker.handleDelivery(context.Background(), item); err != nil {
		t.Fatalf("handleDelivery() error = %v", err)
	}

	if processor.calls != 1 {
		t.Errorf("ImageProcessor.Process() calls = %d, want 1", processor.calls)
	}
	if !reflect.DeepEqual(repository.statusUpdates, []model.ProcessingStatus{model.ProcessingStatusFailed}) {
		t.Errorf("processing status updates = %v, want [failed]", repository.statusUpdates)
	}
	if !reflect.DeepEqual(item.nackCalls, []bool{false}) {
		t.Errorf("delivery nack calls = %v, want [false]", item.nackCalls)
	}
}

func TestWorker_HandleDelivery_RejectsInvalidMessage(t *testing.T) {
	repository := &fakeRepository{}
	storage := &fakeStorage{}
	processor := &fakeImageProcessor{}
	item := &fakeDelivery{
		body:       []byte(`{"schema_version":69}`),
		messageID:  testMessageID,
		routingKey: event.AvatarUploadedRoutingKey,
	}
	avatarWorker := newTestWorker(repository, storage, processor)

	if err := avatarWorker.handleDelivery(context.Background(), item); err != nil {
		t.Fatalf("handleDelivery() error = %v", err)
	}

	if item.ackCalls != 0 || !reflect.DeepEqual(item.nackCalls, []bool{false}) {
		t.Fatalf("delivery ack/nack = %d/%v, want 0/[false]", item.ackCalls, item.nackCalls)
	}
	if repository.claimCalls != 0 || storage.getCalls != 0 || processor.calls != 0 {
		t.Errorf(
			"invalid message side effects: claim=%d get=%d process=%d, want all zero",
			repository.claimCalls,
			storage.getCalls,
			processor.calls,
		)
	}
}

func TestWorker_HandleDelivery_DeletesAvatarFiles(t *testing.T) {
	storage := &fakeStorage{}
	message := testDeletedEvent()
	item := newFakeDeletedDelivery(t, message)
	avatarWorker := newTestWorker(&fakeRepository{}, storage, &fakeImageProcessor{})

	if err := avatarWorker.handleDelivery(context.Background(), item); err != nil {
		t.Fatalf("handleDelivery() error = %v", err)
	}

	if item.ackCalls != 1 || len(item.nackCalls) != 0 {
		t.Fatalf("delivery ack/nack = %d/%v, want 1/[]", item.ackCalls, item.nackCalls)
	}
	if storage.deleteCalls != 1 {
		t.Errorf("Storage.Delete() calls = %d, want 1", storage.deleteCalls)
	}
	if !reflect.DeepEqual(storage.deletedKeys, message.S3Keys) {
		t.Errorf("Storage.Delete() keys = %v, want %v", storage.deletedKeys, message.S3Keys)
	}
}

func TestWorker_HandleDelivery_RetriesAvatarFileDeletion(t *testing.T) {
	storage := &fakeStorage{
		deleteErrors: []error{
			errors.New("temporary S3 error"),
			errors.New("temporary S3 error"),
			nil,
		},
	}
	item := newFakeDeletedDelivery(t, testDeletedEvent())
	avatarWorker := newTestWorker(&fakeRepository{}, storage, &fakeImageProcessor{})

	if err := avatarWorker.handleDelivery(context.Background(), item); err != nil {
		t.Fatalf("handleDelivery() error = %v", err)
	}

	if storage.deleteCalls != maxRetryAttempts {
		t.Errorf("Storage.Delete() calls = %d, want %d", storage.deleteCalls, maxRetryAttempts)
	}
	if item.ackCalls != 1 || len(item.nackCalls) != 0 {
		t.Errorf("delivery ack/nack = %d/%v, want 1/[]", item.ackCalls, item.nackCalls)
	}
}

func TestWorker_HandleDelivery_DeadLettersAvatarDeletionAfterRetries(t *testing.T) {
	storage := &fakeStorage{
		deleteErrors: []error{
			errors.New("S3 unavailable"),
			errors.New("S3 unavailable"),
			errors.New("S3 unavailable"),
		},
	}
	item := newFakeDeletedDelivery(t, testDeletedEvent())
	avatarWorker := newTestWorker(&fakeRepository{}, storage, &fakeImageProcessor{})

	if err := avatarWorker.handleDelivery(context.Background(), item); err != nil {
		t.Fatalf("handleDelivery() error = %v", err)
	}

	if storage.deleteCalls != maxRetryAttempts {
		t.Errorf("Storage.Delete() calls = %d, want %d", storage.deleteCalls, maxRetryAttempts)
	}
	if item.ackCalls != 0 || !reflect.DeepEqual(item.nackCalls, []bool{false}) {
		t.Errorf("delivery ack/nack = %d/%v, want 0/[false]", item.ackCalls, item.nackCalls)
	}
}

func TestWorker_HandleDelivery_CleansThumbnailsWhenAvatarDeletedDuringProcessing(t *testing.T) {
	repository := &fakeRepository{
		claimResults: []claimResult{{claimed: true}},
		completeErr:  model.ErrAvatarNotFound,
	}
	storage := &fakeStorage{content: []byte("original")}
	processor := &fakeImageProcessor{thumbnails: testThumbnails()}
	message := testEvent()
	item := newFakeUploadedDelivery(t, message)
	avatarWorker := newTestWorker(repository, storage, processor)

	if err := avatarWorker.handleDelivery(context.Background(), item); err != nil {
		t.Fatalf("handleDelivery() error = %v", err)
	}

	if repository.completeCalls != 1 {
		t.Errorf("CompleteProcessing() calls = %d, want 1", repository.completeCalls)
	}
	if item.ackCalls != 1 || len(item.nackCalls) != 0 {
		t.Fatalf("delivery ack/nack = %d/%v, want 1/[]", item.ackCalls, item.nackCalls)
	}
	wantDeleted := []string{
		s3.ThumbnailKey("Alice", testAvatarID, model.ThumbnailSize100x100),
		s3.ThumbnailKey("Alice", testAvatarID, model.ThumbnailSize300x300),
	}
	if !reflect.DeepEqual(storage.deletedKeys, wantDeleted) {
		t.Errorf("Storage.Delete() keys = %v, want %v", storage.deletedKeys, wantDeleted)
	}
	if len(repository.statusUpdates) != 0 {
		t.Errorf("processing status updates = %v, want none", repository.statusUpdates)
	}
}

func TestWorker_HandleDelivery_RequeuesClaimOnShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	repository := &fakeRepository{claimResults: []claimResult{{claimed: true}}}
	storage := &fakeStorage{
		getFunc: func(ctx context.Context, _ string) (io.ReadCloser, error) {
			cancel()
			<-ctx.Done()

			return nil, ctx.Err()
		},
	}
	processor := &fakeImageProcessor{}
	item := newFakeUploadedDelivery(t, testEvent())
	avatarWorker := newTestWorker(repository, storage, processor)

	if err := avatarWorker.handleDelivery(ctx, item); err != nil {
		t.Fatalf("handleDelivery() error = %v", err)
	}

	if item.ackCalls != 0 || !reflect.DeepEqual(item.nackCalls, []bool{true}) {
		t.Fatalf("delivery ack/nack = %d/%v, want 0/[true]", item.ackCalls, item.nackCalls)
	}
	if !reflect.DeepEqual(repository.statusUpdates, []model.ProcessingStatus{model.ProcessingStatusPending}) {
		t.Errorf("processing status updates = %v, want [pending]", repository.statusUpdates)
	}
}

func TestWorker_HandleDelivery_DeadLettersWhenShutdownRecoveryFails(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	repository := &fakeRepository{
		claimResults: []claimResult{{claimed: true}},
		updateStatusErrors: []error{
			errors.New("PostgreSQL unavailable"),
			errors.New("PostgreSQL unavailable"),
			errors.New("PostgreSQL unavailable"),
		},
	}
	storage := &fakeStorage{
		getFunc: func(ctx context.Context, _ string) (io.ReadCloser, error) {
			cancel()
			<-ctx.Done()

			return nil, ctx.Err()
		},
	}
	item := newFakeUploadedDelivery(t, testEvent())
	avatarWorker := newTestWorker(repository, storage, &fakeImageProcessor{})

	if err := avatarWorker.handleDelivery(ctx, item); err != nil {
		t.Fatalf("handleDelivery() error = %v", err)
	}

	wantStatuses := []model.ProcessingStatus{
		model.ProcessingStatusPending,
		model.ProcessingStatusPending,
		model.ProcessingStatusPending,
	}
	if !reflect.DeepEqual(repository.statusUpdates, wantStatuses) {
		t.Errorf("processing status updates = %v, want %v", repository.statusUpdates, wantStatuses)
	}
	if !reflect.DeepEqual(item.nackCalls, []bool{false}) {
		t.Errorf("delivery nack calls = %v, want [false]", item.nackCalls)
	}
}

func (*fakeConsumer) Consume(ctx context.Context) (<-chan broker.Delivery, error) {
	deliveries := make(chan broker.Delivery)
	go func() {
		<-ctx.Done()
		close(deliveries)
	}()

	return deliveries, nil
}

func (f *fakeDelivery) Body() []byte {
	return f.body
}

func (f *fakeDelivery) MessageID() string {
	return f.messageID
}

func (f *fakeDelivery) RoutingKey() string {
	return f.routingKey
}

func (f *fakeDelivery) Ack() error {
	f.ackCalls++

	return nil
}

func (f *fakeDelivery) Nack(requeue bool) error {
	f.nackCalls = append(f.nackCalls, requeue)

	return nil
}

func (f *fakeRepository) ClaimForProcessing(context.Context, string) (bool, error) {
	index := f.claimCalls
	f.claimCalls++
	if len(f.claimResults) == 0 {
		return false, nil
	}
	if index >= len(f.claimResults) {
		index = len(f.claimResults) - 1
	}

	return f.claimResults[index].claimed, f.claimResults[index].err
}

func (f *fakeRepository) CompleteProcessing(
	_ context.Context,
	_ string,
	thumbnailS3Keys map[model.ThumbnailSize]string,
) error {
	f.completeCalls++
	f.completedKeys = cloneThumbnailKeys(thumbnailS3Keys)

	return f.completeErr
}

func (f *fakeRepository) UpdateProcessingStatus(
	ctx context.Context,
	avatarID string,
	status model.ProcessingStatus,
) error {
	f.statusUpdates = append(f.statusUpdates, status)
	if f.updateStatusFunc != nil {
		return f.updateStatusFunc(ctx, avatarID, status)
	}

	index := len(f.statusUpdates) - 1
	if index < len(f.updateStatusErrors) {
		return f.updateStatusErrors[index]
	}

	return nil
}

func (f *fakeStorage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	f.getCalls++
	f.getKeys = append(f.getKeys, key)
	if f.getFunc != nil {
		return f.getFunc(ctx, key)
	}

	if index := f.getCalls - 1; index < len(f.getErrors) && f.getErrors[index] != nil {
		return nil, f.getErrors[index]
	}

	return io.NopCloser(bytes.NewReader(f.content)), nil
}

func (f *fakeStorage) Put(
	_ context.Context,
	key string,
	reader io.Reader,
	_ int64,
	contentType string,
) error {
	content, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	f.puts = append(f.puts, putCall{key: key, content: content, contentType: contentType})

	return f.putErr
}

func (f *fakeStorage) Delete(ctx context.Context, keys ...string) error {
	f.deleteCalls++
	f.deletedKeys = append(f.deletedKeys, keys...)
	if f.deleteFunc != nil {
		return f.deleteFunc(ctx, keys...)
	}

	if index := f.deleteCalls - 1; index < len(f.deleteErrors) {
		return f.deleteErrors[index]
	}

	return nil
}

func (f *fakeImageProcessor) Process(io.Reader) ([]imageprocessor.Thumbnail, error) {
	f.calls++

	return f.thumbnails, f.err
}

func newTestWorker(repository Repository, storage Storage, processor ImageProcessor) *Worker {
	avatarWorker := New(&fakeConsumer{}, repository, storage, processor, zap.NewNop())
	avatarWorker.retry = retryPolicy{
		maxAttempts:    maxRetryAttempts,
		initialBackoff: 0,
	}

	return avatarWorker
}

func testEvent() event.AvatarUploaded {
	return event.AvatarUploaded{
		MessageID:     testMessageID,
		AvatarID:      testAvatarID,
		UserID:        "Alice",
		S3Key:         "originals/Alice/" + testAvatarID + "/avatar.webp",
		SchemaVersion: event.AvatarUploadedSchemaVersion,
		CreatedAt:     time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC),
	}
}

func testDeletedEvent() event.AvatarDeleted {
	return event.AvatarDeleted{
		MessageID: testMessageID,
		AvatarID:  testAvatarID,
		S3Keys: []string{
			"originals/Alice/" + testAvatarID + "/avatar.webp",
			s3.ThumbnailKey("Alice", testAvatarID, model.ThumbnailSize100x100),
			s3.ThumbnailKey("Alice", testAvatarID, model.ThumbnailSize300x300),
		},
		SchemaVersion: event.AvatarDeletedSchemaVersion,
		CreatedAt:     time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC),
	}
}

func testThumbnails() []imageprocessor.Thumbnail {
	return []imageprocessor.Thumbnail{
		{
			Size:        model.ThumbnailSize100x100,
			ContentType: "image/jpeg",
			Content:     []byte("small thumbnail"),
		},
		{
			Size:        model.ThumbnailSize300x300,
			ContentType: "image/jpeg",
			Content:     []byte("large thumbnail"),
		},
	}
}

func newFakeUploadedDelivery(t *testing.T, message event.AvatarUploaded) *fakeDelivery {
	t.Helper()

	body, err := json.Marshal(message)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}

	return &fakeDelivery{
		body:       body,
		messageID:  message.MessageID,
		routingKey: event.AvatarUploadedRoutingKey,
	}
}

func newFakeDeletedDelivery(t *testing.T, message event.AvatarDeleted) *fakeDelivery {
	t.Helper()

	body, err := json.Marshal(message)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}

	return &fakeDelivery{
		body:       body,
		messageID:  message.MessageID,
		routingKey: event.AvatarDeletedRoutingKey,
	}
}

func cloneThumbnailKeys(source map[model.ThumbnailSize]string) map[model.ThumbnailSize]string {
	result := make(map[model.ThumbnailSize]string, len(source))
	for size, key := range source {
		result[size] = key
	}

	return result
}

func logMessages(t *testing.T, output *bytes.Buffer) []string {
	t.Helper()

	scanner := bufio.NewScanner(output)
	var messages []string
	for scanner.Scan() {
		var entry map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			t.Fatalf("decode log entry: %v", err)
		}
		message, _ := entry["msg"].(string)
		messages = append(messages, message)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan log output: %v", err)
	}

	return messages
}

func testLogger(output *bytes.Buffer) *zap.Logger {
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		zapcore.AddSync(output),
		zapcore.DebugLevel,
	)

	return zap.New(core).With(zap.String("service", "worker"))
}
