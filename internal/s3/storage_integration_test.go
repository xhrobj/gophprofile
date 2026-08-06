//go:build integration

package s3_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/xhrobj/gophprofile/internal/model"
	"github.com/xhrobj/gophprofile/internal/s3"
)

const s3IntegrationTestTimeout = 30 * time.Second

func TestIntegration_MinIOAvatarStorage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), s3IntegrationTestTimeout)
	t.Cleanup(cancel)

	endpoint := requireEnv(t, "S3_ENDPOINT")
	accessKey := requireEnv(t, "S3_ACCESS_KEY")
	secretKey := requireEnv(t, "S3_SECRET_KEY")
	useSSL, err := strconv.ParseBool(requireEnv(t, "S3_USE_SSL"))
	if err != nil {
		t.Fatalf("parse S3_USE_SSL: %v", err)
	}

	bucket := fmt.Sprintf("gophprofile-test-%d", time.Now().UnixNano())
	cleanupClient := newMinIOClient(t, endpoint, accessKey, secretKey, useSSL)
	t.Cleanup(func() {
		cleanupBucket(t, cleanupClient, bucket)
	})

	storage, err := s3.Open(ctx, endpoint, accessKey, secretKey, bucket, useSSL)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := storage.Ping(ctx); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}

	key := s3.OriginalKey("alice", "c0decafe-babe-4bed-b042-feeddeadbeef", "avatar.jpg")
	thumbnailKey := s3.ThumbnailKey("alice", "c0decafe-babe-4bed-b042-feeddeadbeef", model.ThumbnailSize100x100)
	contentType := "image/jpeg"
	want := []byte("avatar-image-data")

	if err := storage.Put(ctx, key, bytes.NewReader(want), int64(len(want)), contentType); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	reader, err := storage.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	got, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil {
		t.Fatalf("read object: %v", readErr)
	}
	if closeErr != nil {
		t.Fatalf("close object: %v", closeErr)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("Get() data = %q, want %q", got, want)
	}

	info, err := cleanupClient.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
	if err != nil {
		t.Fatalf("StatObject() error = %v", err)
	}
	if info.ContentType != contentType {
		t.Errorf("ContentType = %q, want %q", info.ContentType, contentType)
	}

	if err := storage.Put(ctx, thumbnailKey, bytes.NewReader(want), int64(len(want)), contentType); err != nil {
		t.Fatalf("Put() thumbnail error = %v", err)
	}

	if err := storage.Delete(ctx, key, thumbnailKey); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if err := storage.Delete(ctx, key, thumbnailKey); err != nil {
		t.Fatalf("second Delete() error = %v", err)
	}
	if _, err := storage.Get(ctx, key); err == nil {
		t.Error("Get() deleted original error = nil, want error")
	}
	if _, err := storage.Get(ctx, thumbnailKey); err == nil {
		t.Error("Get() deleted thumbnail error = nil, want error")
	}
}

func requireEnv(t *testing.T, name string) string {
	t.Helper()

	value := os.Getenv(name)
	if value == "" {
		t.Fatalf("%s is not set", name)
	}

	return value
}

func newMinIOClient(
	t *testing.T,
	endpoint string,
	accessKey string,
	secretKey string,
	useSSL bool,
) *minio.Client {
	t.Helper()

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		t.Fatalf("create MinIO cleanup client: %v", err)
	}

	return client
}

func cleanupBucket(t *testing.T, client *minio.Client, bucket string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), s3IntegrationTestTimeout)
	defer cancel()

	for object := range client.ListObjects(ctx, bucket, minio.ListObjectsOptions{Recursive: true}) {
		if object.Err != nil {
			t.Errorf("list MinIO test objects: %v", object.Err)
			continue
		}
		if err := client.RemoveObject(ctx, bucket, object.Key, minio.RemoveObjectOptions{}); err != nil {
			t.Errorf("remove MinIO test object %q: %v", object.Key, err)
		}
	}
	if err := client.RemoveBucket(ctx, bucket); err != nil {
		t.Errorf("remove MinIO test bucket %q: %v", bucket, err)
	}
}
