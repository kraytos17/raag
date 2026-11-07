package peer

import (
	"errors"
	"fmt"
	"testing"

	pb "github.com/p-society/raag/proto/gen"
)

func TestErrorCode(t *testing.T) {
	syncErr := &SyncError{Code: pb.ErrorCode_ERROR_CODE_PERMISSION_DENIED, Message: "library sharing disabled"}
	if code, ok := ErrorCode(syncErr); !ok || code != pb.ErrorCode_ERROR_CODE_PERMISSION_DENIED {
		t.Errorf("ErrorCode() = (%v, %v), want (PERMISSION_DENIED, true)", code, ok)
	}

	wrapped := fmt.Errorf("fetch: %w", syncErr)
	if code, ok := ErrorCode(wrapped); !ok || code != pb.ErrorCode_ERROR_CODE_PERMISSION_DENIED {
		t.Errorf("ErrorCode(wrapped) = (%v, %v), want (PERMISSION_DENIED, true)", code, ok)
	}

	if code, ok := ErrorCode(errors.New("boom")); ok || code != pb.ErrorCode_ERROR_CODE_UNSPECIFIED {
		t.Errorf("ErrorCode(plain) = (%v, %v), want (UNSPECIFIED, false)", code, ok)
	}

	if code, ok := ErrorCode(nil); ok || code != pb.ErrorCode_ERROR_CODE_UNSPECIFIED {
		t.Errorf("ErrorCode(nil) = (%v, %v), want (UNSPECIFIED, false)", code, ok)
	}
}
