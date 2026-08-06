package handler

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/google/uuid"
)

const (
	maxUserIDBytes   = 255
	maxFileNameBytes = 255
)

func validateAvatarID(value string) error {
	if _, err := uuid.Parse(value); err != nil {
		return errors.New("avatar_id must be a valid UUID")
	}

	return nil
}

func validateUserID(value, field string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if len(value) > maxUserIDBytes {
		return "", fmt.Errorf("%s must not exceed %d bytes", field, maxUserIDBytes)
	}
	for _, symbol := range value {
		if unicode.IsControl(symbol) {
			return "", fmt.Errorf("%s must not contain control characters", field)
		}
	}

	return value, nil
}

func validateFileName(value string) error {
	if len(value) > maxFileNameBytes {
		return fmt.Errorf("file name must not exceed %d bytes", maxFileNameBytes)
	}

	return nil
}
