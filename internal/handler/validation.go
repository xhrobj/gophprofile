package handler

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	maxUserIDLen   = 255
	maxFileNameLen = 255
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
	if utf8.RuneCountInString(value) > maxUserIDLen {
		return "", fmt.Errorf("%s must not exceed %d characters", field, maxUserIDLen)
	}
	for _, symbol := range value {
		if unicode.IsControl(symbol) {
			return "", fmt.Errorf("%s must not contain control characters", field)
		}
	}

	return value, nil
}

func validateFileName(value string) error {
	if utf8.RuneCountInString(value) > maxFileNameLen {
		return fmt.Errorf("file name must not exceed %d characters", maxFileNameLen)
	}

	return nil
}
