package service

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"testing"
)

const (
	testImageWidth  = 3
	testImageHeight = 2
)

func TestInspectImage(t *testing.T) {
	tests := []struct {
		name         string
		content      func(t *testing.T) []byte
		wantMIMEType string
		wantWidth    int
		wantHeight   int
		wantErr      error
	}{
		{
			name:         "detects JPEG",
			content:      encodeJPEG,
			wantMIMEType: mimeTypeJPEG,
			wantWidth:    testImageWidth,
			wantHeight:   testImageHeight,
		},
		{
			name:         "detects PNG",
			content:      encodePNG,
			wantMIMEType: mimeTypePNG,
			wantWidth:    testImageWidth,
			wantHeight:   testImageHeight,
		},
		{
			name:         "detects WebP",
			content:      readWebPFixture,
			wantMIMEType: mimeTypeWebP,
			wantWidth:    testImageWidth,
			wantHeight:   testImageHeight,
		},
		{
			name:    "rejects GIF",
			content: encodeGIF,
			wantErr: ErrInvalidImageFormat,
		},
		{
			name: "rejects malformed image",
			content: func(_ *testing.T) []byte {
				return []byte("not an image")
			},
			wantErr: ErrInvalidImageFormat,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metadata, err := inspectImage(tt.content(t))
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("inspectImage() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("inspectImage() error = %v", err)
			}

			if metadata.mimeType != tt.wantMIMEType {
				t.Errorf("inspectImage() MIMEType = %q, want %q", metadata.mimeType, tt.wantMIMEType)
			}
			if metadata.width != tt.wantWidth || metadata.height != tt.wantHeight {
				t.Errorf(
					"inspectImage() dimensions = %dx%d, want %dx%d",
					metadata.width,
					metadata.height,
					tt.wantWidth,
					tt.wantHeight,
				)
			}
		})
	}
}

func encodeJPEG(t *testing.T) []byte {
	t.Helper()

	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, testImage(), nil); err != nil {
		t.Fatalf("encode JPEG fixture: %v", err)
	}

	return buffer.Bytes()
}

func encodePNG(t *testing.T) []byte {
	t.Helper()

	var buffer bytes.Buffer
	if err := png.Encode(&buffer, testImage()); err != nil {
		t.Fatalf("encode PNG fixture: %v", err)
	}

	return buffer.Bytes()
}

func encodeGIF(t *testing.T) []byte {
	t.Helper()

	var buffer bytes.Buffer
	if err := gif.Encode(&buffer, testImage(), nil); err != nil {
		t.Fatalf("encode GIF fixture: %v", err)
	}

	return buffer.Bytes()
}

func readWebPFixture(t *testing.T) []byte {
	t.Helper()

	content, err := os.ReadFile("testdata/avatar.webp")
	if err != nil {
		t.Fatalf("read WebP fixture: %v", err)
	}

	return content
}

func testImage() image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, testImageWidth, testImageHeight))
	for y := 0; y < testImageHeight; y++ {
		for x := 0; x < testImageWidth; x++ {
			img.Set(x, y, color.NRGBA{R: 42, G: 105, B: 169, A: 255})
		}
	}

	return img
}
