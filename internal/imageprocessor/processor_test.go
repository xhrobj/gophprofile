package imageprocessor

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

	"github.com/xhrobj/gophprofile/internal/model"
)

const (
	testImageWidth  = 6
	testImageHeight = 4
)

func TestProcessor_Process(t *testing.T) {
	tests := []struct {
		name    string
		content func(t *testing.T) []byte
	}{
		{name: "JPEG", content: encodeJPEG},
		{name: "PNG", content: encodePNG},
		{name: "WebP", content: readWebPFixture},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			thumbnails, err := New().Process(bytes.NewReader(tt.content(t)))
			if err != nil {
				t.Fatalf("Process() error = %v", err)
			}

			want := []struct {
				size   model.ThumbnailSize
				pixels int
			}{
				{size: model.ThumbnailSize100x100, pixels: thumbnailSize100},
				{size: model.ThumbnailSize300x300, pixels: thumbnailSize300},
			}
			if len(thumbnails) != len(want) {
				t.Fatalf("Process() thumbnails count = %d, want %d", len(thumbnails), len(want))
			}

			for index, expected := range want {
				thumbnail := thumbnails[index]
				if thumbnail.Size != expected.size {
					t.Errorf("thumbnail %d size = %q, want %q", index, thumbnail.Size, expected.size)
				}
				if thumbnail.ContentType != jpegContentType {
					t.Errorf("thumbnail %d content type = %q, want %q", index, thumbnail.ContentType, jpegContentType)
				}

				decoded, format, err := image.Decode(bytes.NewReader(thumbnail.Content))
				if err != nil {
					t.Fatalf("decode thumbnail %d: %v", index, err)
				}
				if format != "jpeg" {
					t.Errorf("thumbnail %d format = %q, want jpeg", index, format)
				}
				if decoded.Bounds().Dx() != expected.pixels || decoded.Bounds().Dy() != expected.pixels {
					t.Errorf(
						"thumbnail %d dimensions = %dx%d, want %dx%d",
						index,
						decoded.Bounds().Dx(),
						decoded.Bounds().Dy(),
						expected.pixels,
						expected.pixels,
					)
				}
			}
		})
	}
}

func TestProcessor_Process_RejectsInvalidImage(t *testing.T) {
	tests := []struct {
		name    string
		content func(t *testing.T) []byte
	}{
		{
			name: "GIF",
			content: func(t *testing.T) []byte {
				t.Helper()

				var buffer bytes.Buffer
				if err := gif.Encode(&buffer, testImage(), nil); err != nil {
					t.Fatalf("encode GIF fixture: %v", err)
				}

				return buffer.Bytes()
			},
		},
		{
			name: "malformed image",
			content: func(_ *testing.T) []byte {
				return []byte("not an image")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New().Process(bytes.NewReader(tt.content(t)))
			if !errors.Is(err, ErrInvalidImage) {
				t.Fatalf("Process() error = %v, want %v", err, ErrInvalidImage)
			}
		})
	}
}

func TestCenteredSquare(t *testing.T) {
	tests := []struct {
		name   string
		bounds image.Rectangle
		want   image.Rectangle
	}{
		{
			name:   "crops landscape image horizontally",
			bounds: image.Rect(0, 0, 6, 4),
			want:   image.Rect(1, 0, 5, 4),
		},
		{
			name:   "crops portrait image vertically",
			bounds: image.Rect(0, 0, 4, 6),
			want:   image.Rect(0, 1, 4, 5),
		},
		{
			name:   "keeps square image",
			bounds: image.Rect(0, 0, 4, 4),
			want:   image.Rect(0, 0, 4, 4),
		},
		{
			name:   "respects non-zero origin",
			bounds: image.Rect(10, 20, 16, 24),
			want:   image.Rect(11, 20, 15, 24),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := centeredSquare(tt.bounds); got != tt.want {
				t.Errorf("centeredSquare() = %v, want %v", got, tt.want)
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
