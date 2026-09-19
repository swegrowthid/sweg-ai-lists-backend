package validator

import (
	"errors"
	"strings"
)

// Validator holds no state. Keep validation at the edge.
type Validator struct{}

// New builds a Validator.
func New() *Validator { return &Validator{} }

// Required rejects blank strings. Small helper until real rules land.
func (Validator) Required(v string) error {
	if strings.TrimSpace(v) == "" {
		return errors.New("required")
	}
	return nil
}
