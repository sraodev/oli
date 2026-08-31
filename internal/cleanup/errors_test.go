package cleanup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
)

func TestStableErrorCodes(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{
		{context.Canceled, "cancelled"}, {context.DeadlineExceeded, "deadline_exceeded"},
		{ErrProtection, "protection_unavailable"}, {ErrBusy, "busy"}, {ErrInvalidScan, "invalid_scan"},
		{ErrSelection, "invalid_selection"}, {ErrScanOnly, "invalid_selection"}, {ErrNoRulesSelected, "invalid_selection"},
		{ErrScanLimit, "resource_limit"}, {ErrPartial, "partial_failure"}, {os.ErrPermission, "permission_denied"},
		{errors.New("private/path"), "operation_failed"},
	} {
		if got := ErrorCode(fmt.Errorf("detail: %w", tc.err)); got != tc.code {
			t.Fatalf("%v: %s", tc.err, got)
		}
	}
	if ErrorCode(nil) != "" {
		t.Fatal("success has error code")
	}
}
