package s3

import (
	"context"
	"fmt"
	"io"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Storage хранит объекты в одном S3 bucket.
type Storage struct {
	client *minio.Client
	bucket string
}

// Open создаёт S3-клиент и проверяет готовность bucket.
// Если bucket ещё не существует, Open создаёт его.
func Open(
	ctx context.Context,
	endpoint string,
	accessKey string,
	secretKey string,
	bucket string,
	useSSL bool,
) (*Storage, error) {
	transport, err := minio.DefaultTransport(useSSL)
	if err != nil {
		return nil, fmt.Errorf("create S3 transport: %w", err)
	}

	client, err := minio.New(endpoint, &minio.Options{
		Creds:     credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure:    useSSL,
		Transport: otelhttp.NewTransport(transport),
	})
	if err != nil {
		return nil, fmt.Errorf("create S3 client: %w", err)
	}

	storage := &Storage{
		client: client,
		bucket: bucket,
	}

	if err := storage.ensureBucket(ctx); err != nil {
		return nil, err
	}

	return storage, nil
}

// Put сохраняет объект под указанным ключом.
func (s *Storage) Put(
	ctx context.Context,
	key string,
	reader io.Reader,
	size int64,
	contentType string,
) error {
	_, err := s.client.PutObject(
		ctx,
		s.bucket,
		key,
		reader,
		size,
		minio.PutObjectOptions{
			ContentType: contentType,
		},
	)
	if err != nil {
		return fmt.Errorf("put S3 object %q: %w", key, err)
	}

	return nil
}

// Get открывает объект для потокового чтения.
// Вызывающий код обязан закрыть возвращённый reader.
func (s *Storage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	object, err := s.client.GetObject(
		ctx,
		s.bucket,
		key,
		minio.GetObjectOptions{},
	)
	if err != nil {
		return nil, fmt.Errorf("get S3 object %q: %w", key, err)
	}

	if _, err := object.Stat(); err != nil {
		_ = object.Close()

		return nil, fmt.Errorf("stat S3 object %q: %w", key, err)
	}

	return object, nil
}

// Delete удаляет один или несколько объектов.
// Удаление отсутствующего объекта считается успешным.
func (s *Storage) Delete(ctx context.Context, keys ...string) error {
	for _, key := range keys {
		if err := s.client.RemoveObject(
			ctx,
			s.bucket,
			key,
			minio.RemoveObjectOptions{},
		); err != nil {
			return fmt.Errorf("delete S3 object %q: %w", key, err)
		}
	}

	return nil
}

// Ping проверяет доступность настроенного bucket.
func (s *Storage) Ping(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("check S3 bucket %q: %w", s.bucket, err)
	}
	if !exists {
		return fmt.Errorf("S3 bucket %q does not exist", s.bucket)
	}

	return nil
}

func (s *Storage) ensureBucket(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("check S3 bucket %q: %w", s.bucket, err)
	}
	if exists {
		return nil
	}

	makeErr := s.client.MakeBucket(
		ctx,
		s.bucket,
		minio.MakeBucketOptions{},
	)
	if makeErr == nil {
		return nil
	}

	// Между проверкой и созданием bucket другой экземпляр сервиса мог создать его первым.
	exists, checkErr := s.client.BucketExists(ctx, s.bucket)
	if checkErr != nil {
		return fmt.Errorf("recheck S3 bucket %q: %w", s.bucket, checkErr)
	}
	if exists {
		return nil
	}

	return fmt.Errorf("create S3 bucket %q: %w", s.bucket, makeErr)
}
