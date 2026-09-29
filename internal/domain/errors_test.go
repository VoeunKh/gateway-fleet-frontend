package domain

import (
	"errors"
	"testing"
)

func TestValidationErrorIsTyped(t *testing.T) {
	var ve *ValidationError
	if err := ValidateUCI("x"); !errors.As(err, &ve) {
		t.Fatalf("want *ValidationError, got %T", err)
	}
}
