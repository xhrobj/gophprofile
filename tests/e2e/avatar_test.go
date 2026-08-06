//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

const (
	defaultBaseURL = "http://127.0.0.1:8080"
	requestTimeout = 5 * time.Second
	pollTimeout    = 20 * time.Second
	pollInterval   = 100 * time.Millisecond
)

type uploadResponse struct {
	ID     string `json:"id"`
	UserID string `json:"user_id"`
	URL    string `json:"url"`
	Status string `json:"status"`
}

type thumbnailMetadata struct {
	Size string `json:"size"`
	URL  string `json:"url"`
}

type dimensionsMetadata struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type avatarMetadata struct {
	ID               string              `json:"id"`
	UserID           string              `json:"user_id"`
	MIMEType         string              `json:"mime_type"`
	Size             int64               `json:"size"`
	Dimensions       dimensionsMetadata  `json:"dimensions"`
	Thumbnails       []thumbnailMetadata `json:"thumbnails"`
	UploadStatus     string              `json:"upload_status"`
	ProcessingStatus string              `json:"processing_status"`
}

type avatarListResponse struct {
	Avatars []uploadResponse `json:"avatars"`
	Total   int              `json:"total"`
}

func TestE2E_AvatarLifecycle(t *testing.T) {
	baseURL := strings.TrimRight(os.Getenv("E2E_BASE_URL"), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := &http.Client{Timeout: requestTimeout}
	userID := fmt.Sprintf("Alice-e2e-%d", time.Now().UnixNano())
	original := newPNG(t, 640, 480)

	uploaded := uploadAvatar(t, ctx, client, baseURL, userID, original)
	if uploaded.ID == "" {
		t.Fatal("upload response contains empty avatar id")
	}
	if uploaded.UserID != userID {
		t.Fatalf("upload user_id = %q, want %q", uploaded.UserID, userID)
	}
	if uploaded.Status != "processing" {
		t.Fatalf("upload status = %q, want %q", uploaded.Status, "processing")
	}

	metadata := waitForCompletedProcessing(t, ctx, client, baseURL, uploaded.ID)
	if metadata.ID != uploaded.ID {
		t.Fatalf("metadata id = %q, want %q", metadata.ID, uploaded.ID)
	}
	if metadata.UserID != userID {
		t.Fatalf("metadata user_id = %q, want %q", metadata.UserID, userID)
	}
	if metadata.MIMEType != "image/png" {
		t.Fatalf("metadata mime_type = %q, want %q", metadata.MIMEType, "image/png")
	}
	if metadata.UploadStatus != "completed" {
		t.Fatalf("upload_status = %q, want %q", metadata.UploadStatus, "completed")
	}
	if metadata.Size != int64(len(original)) {
		t.Fatalf("metadata size = %d, want %d", metadata.Size, len(original))
	}
	if metadata.Dimensions.Width != 640 || metadata.Dimensions.Height != 480 {
		t.Fatalf(
			"metadata dimensions = %dx%d, want 640x480",
			metadata.Dimensions.Width,
			metadata.Dimensions.Height,
		)
	}
	assertThumbnailMetadata(t, metadata.Thumbnails)

	originalDownloaded := downloadAvatar(t, ctx, client, baseURL, uploaded.ID, "original", "image/png")
	if !bytes.Equal(originalDownloaded, original) {
		t.Fatal("downloaded original differs from uploaded image")
	}

	assertThumbnail(t, downloadAvatar(t, ctx, client, baseURL, uploaded.ID, "100x100", "image/jpeg"), 100)
	assertThumbnail(t, downloadAvatar(t, ctx, client, baseURL, uploaded.ID, "300x300", "image/jpeg"), 300)

	deleteAvatar(t, ctx, client, baseURL, uploaded.ID, userID)
	assertNotFound(t, ctx, client, baseURL+"/api/v1/avatars/"+uploaded.ID+"/metadata")
	assertNotFound(t, ctx, client, baseURL+"/api/v1/avatars/"+uploaded.ID)
	assertUserHasNoAvatars(t, ctx, client, baseURL, userID)
}

func uploadAvatar(
	t *testing.T,
	ctx context.Context,
	client *http.Client,
	baseURL string,
	userID string,
	content []byte,
) uploadResponse {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "avatar.png")
	if err != nil {
		t.Fatalf("create multipart file: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write multipart file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/v1/avatars", &body)
	if err != nil {
		t.Fatalf("create upload request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-User-ID", userID)

	responseBody := doRequest(t, client, req, http.StatusCreated, "application/json")

	var response uploadResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}

	return response
}

func waitForCompletedProcessing(
	t *testing.T,
	ctx context.Context,
	client *http.Client,
	baseURL string,
	avatarID string,
) avatarMetadata {
	t.Helper()

	pollCtx, cancel := context.WithTimeout(ctx, pollTimeout)
	defer cancel()

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		metadata := getMetadata(t, pollCtx, client, baseURL, avatarID, http.StatusOK)
		switch metadata.ProcessingStatus {
		case "completed":
			return metadata
		case "failed":
			t.Fatal("avatar processing failed")
		}

		select {
		case <-pollCtx.Done():
			t.Fatalf("wait for avatar processing: %v", pollCtx.Err())
		case <-ticker.C:
		}
	}
}

func getMetadata(
	t *testing.T,
	ctx context.Context,
	client *http.Client,
	baseURL string,
	avatarID string,
	wantStatus int,
) avatarMetadata {
	t.Helper()

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		baseURL+"/api/v1/avatars/"+avatarID+"/metadata",
		nil,
	)
	if err != nil {
		t.Fatalf("create metadata request: %v", err)
	}

	responseBody := doRequest(t, client, req, wantStatus, "application/json")
	if wantStatus != http.StatusOK {
		return avatarMetadata{}
	}

	var response avatarMetadata
	if err := json.Unmarshal(responseBody, &response); err != nil {
		t.Fatalf("decode metadata response: %v", err)
	}

	return response
}

func downloadAvatar(
	t *testing.T,
	ctx context.Context,
	client *http.Client,
	baseURL string,
	avatarID string,
	size string,
	contentType string,
) []byte {
	t.Helper()

	url := baseURL + "/api/v1/avatars/" + avatarID
	if size != "original" {
		url += "?size=" + size
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("create download request: %v", err)
	}

	return doRequest(t, client, req, http.StatusOK, contentType)
}

func deleteAvatar(
	t *testing.T,
	ctx context.Context,
	client *http.Client,
	baseURL string,
	avatarID string,
	userID string,
) {
	t.Helper()

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, baseURL+"/api/v1/avatars/"+avatarID, nil)
	if err != nil {
		t.Fatalf("create delete request: %v", err)
	}
	req.Header.Set("X-User-ID", userID)

	doRequest(t, client, req, http.StatusNoContent, "")
}

func assertNotFound(t *testing.T, ctx context.Context, client *http.Client, url string) {
	t.Helper()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("create not-found request: %v", err)
	}

	doRequest(t, client, req, http.StatusNotFound, "application/json")
}

func assertUserHasNoAvatars(
	t *testing.T,
	ctx context.Context,
	client *http.Client,
	baseURL string,
	userID string,
) {
	t.Helper()

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		baseURL+"/api/v1/users/"+userID+"/avatars",
		nil,
	)
	if err != nil {
		t.Fatalf("create list request: %v", err)
	}

	responseBody := doRequest(t, client, req, http.StatusOK, "application/json")

	var response avatarListResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if response.Total != 0 || len(response.Avatars) != 0 {
		t.Fatalf("avatar list after delete = total %d, avatars %d; want empty", response.Total, len(response.Avatars))
	}
}

func assertThumbnailMetadata(t *testing.T, thumbnails []thumbnailMetadata) {
	t.Helper()

	got := make(map[string]bool, len(thumbnails))
	for _, thumbnail := range thumbnails {
		got[thumbnail.Size] = thumbnail.URL != ""
	}

	for _, size := range []string{"100x100", "300x300"} {
		if !got[size] {
			t.Fatalf("metadata does not contain usable %s thumbnail", size)
		}
	}
}

func assertThumbnail(t *testing.T, content []byte, wantSize int) {
	t.Helper()

	config, format, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil {
		t.Fatalf("decode thumbnail: %v", err)
	}
	if format != "jpeg" {
		t.Fatalf("thumbnail format = %q, want %q", format, "jpeg")
	}
	if config.Width != wantSize || config.Height != wantSize {
		t.Fatalf("thumbnail dimensions = %dx%d, want %dx%d", config.Width, config.Height, wantSize, wantSize)
	}
}

func doRequest(
	t *testing.T,
	client *http.Client,
	req *http.Request,
	wantStatus int,
	wantContentType string,
) []byte {
	t.Helper()

	response, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", req.Method, req.URL, err)
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			t.Errorf("close %s %s response: %v", req.Method, req.URL, err)
		}
	}()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read %s %s response: %v", req.Method, req.URL, err)
	}
	if response.StatusCode != wantStatus {
		t.Fatalf(
			"%s %s status = %d, want %d; body: %s",
			req.Method,
			req.URL,
			response.StatusCode,
			wantStatus,
			strings.TrimSpace(string(body)),
		)
	}
	if wantContentType != "" {
		contentType := response.Header.Get("Content-Type")
		if !strings.HasPrefix(contentType, wantContentType) {
			t.Fatalf(
				"%s %s Content-Type = %q, want prefix %q",
				req.Method,
				req.URL,
				contentType,
				wantContentType,
			)
		}
	}

	return body
}

func newPNG(t *testing.T, width, height int) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, width, height))

	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatalf("encode PNG: %v", err)
	}

	return buffer.Bytes()
}
