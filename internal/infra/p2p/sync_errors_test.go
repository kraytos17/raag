package p2p

import (
	"errors"
	"fmt"
	"testing"

	pb "github.com/p-society/raag/proto/gen"
)

func TestSyncErrorCode(t *testing.T) {
	syncErr := &SyncError{Code: pb.ErrorCode_ERROR_CODE_PERMISSION_DENIED, Message: "library sharing disabled"}
	if code, ok := SyncErrorCode(syncErr); !ok || code != pb.ErrorCode_ERROR_CODE_PERMISSION_DENIED {
		t.Errorf("SyncErrorCode() = (%v, %v), want (PERMISSION_DENIED, true)", code, ok)
	}

	wrapped := fmt.Errorf("fetch: %w", syncErr)
	if code, ok := SyncErrorCode(wrapped); !ok || code != pb.ErrorCode_ERROR_CODE_PERMISSION_DENIED {
		t.Errorf("SyncErrorCode(wrapped) = (%v, %v), want (PERMISSION_DENIED, true)", code, ok)
	}

	if code, ok := SyncErrorCode(errors.New("boom")); ok || code != pb.ErrorCode_ERROR_CODE_UNSPECIFIED {
		t.Errorf("SyncErrorCode(plain) = (%v, %v), want (UNSPECIFIED, false)", code, ok)
	}

	if code, ok := SyncErrorCode(nil); ok || code != pb.ErrorCode_ERROR_CODE_UNSPECIFIED {
		t.Errorf("SyncErrorCode(nil) = (%v, %v), want (UNSPECIFIED, false)", code, ok)
	}
}
