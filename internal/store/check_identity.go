package store

import (
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

var ErrInvalidCheckID = errors.New("store: invalid check id")

func normalizeCheckID(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("%w: missing value", ErrInvalidCheckID)
	}

	parsed, err := uuid.Parse(id)
	if err != nil || parsed == uuid.Nil {
		return "", fmt.Errorf("%w: %q", ErrInvalidCheckID, id)
	}
	return parsed.String(), nil
}
