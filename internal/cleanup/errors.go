package cleanup

import (
	"context"
	"errors"
	"os"
)

var (
	ErrBusy      = errors.New("another operation is running")
	ErrSelection = errors.New("invalid selection")
	ErrPartial   = errors.New("cleanup had failed or changed candidates")
)

// ErrorCode is a stable client contract. Error strings remain local detail.
func ErrorCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, ErrProtection):
		return "protection_unavailable"
	case errors.Is(err, ErrBusy):
		return "busy"
	case errors.Is(err, ErrInvalidScan):
		return "invalid_scan"
	case errors.Is(err, ErrSelection), errors.Is(err, ErrNoRulesSelected), errors.Is(err, ErrScanOnly):
		return "invalid_selection"
	case errors.Is(err, ErrScanLimit):
		return "resource_limit"
	case errors.Is(err, ErrPartial):
		return "partial_failure"
	case errors.Is(err, os.ErrPermission):
		return "permission_denied"
	default:
		return "operation_failed"
	}
}
