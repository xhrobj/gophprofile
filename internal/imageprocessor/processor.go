package imageprocessor

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"

	"github.com/xhrobj/gophprofile/internal/model"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	jpegContentType = "image/jpeg"

	jpegQuality      = 85
	thumbnailSize100 = 100
	thumbnailSize300 = 300
)

// Thumbnail содержит готовую JPEG-миниатюру.
type Thumbnail struct {
	Size        model.ThumbnailSize
	ContentType string
	Content     []byte
}

// Processor создаёт квадратные миниатюры аватаров.
type Processor struct{}

// ErrInvalidImage означает, что исходные данные нельзя обработать как поддерживаемое изображение.
var ErrInvalidImage = errors.New("invalid image")

// New создаёт image processor.
func New() *Processor {
	return &Processor{}
}

// Process декодирует JPEG, PNG или WebP, вырезает центральный квадрат и создаёт JPEG-миниатюры.
func (*Processor) Process(reader io.Reader) ([]Thumbnail, error) {
	source, format, err := image.Decode(reader)
	if err != nil {
		return nil, fmt.Errorf("%w: decode source: %v", ErrInvalidImage, err)
	}
	if !supportedFormat(format) {
		return nil, fmt.Errorf("%w: unsupported format %q", ErrInvalidImage, format)
	}

	sourceBounds := centeredSquare(source.Bounds())
	thumbnails := make([]Thumbnail, 0, 2)
	for _, spec := range []struct {
		size   model.ThumbnailSize
		pixels int
	}{
		{size: model.ThumbnailSize100x100, pixels: thumbnailSize100},
		{size: model.ThumbnailSize300x300, pixels: thumbnailSize300},
	} {
		thumbnail, err := resizeAndEncode(source, sourceBounds, spec.size, spec.pixels)
		if err != nil {
			return nil, err
		}

		thumbnails = append(thumbnails, thumbnail)
	}

	return thumbnails, nil
}

func supportedFormat(format string) bool {
	switch format {
	case "jpeg", "png", "webp":
		return true
	default:
		return false
	}
}

func centeredSquare(bounds image.Rectangle) image.Rectangle {
	width := bounds.Dx()
	height := bounds.Dy()
	side := min(width, height)

	x := bounds.Min.X + (width-side)/2
	y := bounds.Min.Y + (height-side)/2

	return image.Rect(x, y, x+side, y+side)
}

func resizeAndEncode(
	source image.Image,
	sourceBounds image.Rectangle,
	size model.ThumbnailSize,
	pixels int,
) (Thumbnail, error) {
	destination := image.NewRGBA(image.Rect(0, 0, pixels, pixels))
	draw.CatmullRom.Scale(
		destination,
		destination.Bounds(),
		source,
		sourceBounds,
		draw.Src,
		nil,
	)

	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, destination, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return Thumbnail{}, fmt.Errorf("encode %s thumbnail: %w", size, err)
	}

	return Thumbnail{
		Size:        size,
		ContentType: jpegContentType,
		Content:     buffer.Bytes(),
	}, nil
}
