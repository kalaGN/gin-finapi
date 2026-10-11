package mongo

import (
	"context"
	"errors"
	"fmt"
)

var (
	ErrNotFound    = errors.New("MongoDB document not found")
	ErrInvalidData = errors.New("invalid MongoDB document data")
	ErrUnavailable = errors.New("MongoDB unavailable")
)

func classifyMongoError(action string, err error) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return fmt.Errorf("MongoDB %s: %w", action, err)
	}
	return fmt.Errorf("%w during %s: %w", ErrUnavailable, action, err)
}
