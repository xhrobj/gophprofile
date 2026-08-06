package s3

import (
	"fmt"

	"github.com/xhrobj/gophprofile/internal/model"
)

// OriginalKey возвращает канонический S3 key оригинала аватарки.
func OriginalKey(userID, avatarID, fileName string) string {
	return fmt.Sprintf("originals/%s/%s/%s", userID, avatarID, fileName)
}

// ThumbnailKey возвращает канонический S3 key миниатюры аватарки.
func ThumbnailKey(userID, avatarID string, size model.ThumbnailSize) string {
	return fmt.Sprintf("thumbnails/%s/%s/%s.jpg", userID, avatarID, size)
}
