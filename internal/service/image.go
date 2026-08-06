package service

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // регистрирует JPEG-декодер для image.DecodeConfig
	_ "image/png"  // регистрирует PNG-декодер для image.DecodeConfig

	_ "golang.org/x/image/webp" // регистрирует WebP-декодер для image.DecodeConfig
)

const (
	mimeTypeJPEG = "image/jpeg"
	mimeTypePNG  = "image/png"
	mimeTypeWebP = "image/webp"
)

type imageMetadata struct {
	mimeType string
	width    int
	height   int
}

// ErrInvalidImageFormat означает, что содержимое файла не является поддерживаемым изображением.
var ErrInvalidImageFormat = errors.New("invalid image format")

func inspectImage(content []byte) (imageMetadata, error) {
	config, format, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil {
		return imageMetadata{}, fmt.Errorf("%w: %v", ErrInvalidImageFormat, err)
	}

	var mimeType string
	switch format {
	case "jpeg":
		mimeType = mimeTypeJPEG
	case "png":
		mimeType = mimeTypePNG
	case "webp":
		mimeType = mimeTypeWebP
	default:
		return imageMetadata{}, fmt.Errorf("%w: unsupported format %q", ErrInvalidImageFormat, format)
	}

	return imageMetadata{
		mimeType: mimeType,
		width:    config.Width,
		height:   config.Height,
	}, nil
}
