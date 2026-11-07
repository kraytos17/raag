package peer

import (
	"errors"

	pb "github.com/p-society/raag/proto/gen"
)

// SyncError is a protocol-level error returned by a peer in place of the
// requested sync payload. It preserves the wire ErrorCode so callers can
// distinguish permanent rejections (e.g. sharing disabled) from transient
// transport failures without matching on message strings.
type SyncError struct {
	Code    pb.ErrorCode
	Message string
}

func (e *SyncError) Error() string { return e.Message }

// SyncErrorCode unwraps err and reports the peer's protocol error code.
// Plain transport errors (EOF, resets, timeouts) report UNSPECIFIED with ok
// set to false so callers treat them as transient and retryable.
func ErrorCode(err error) (pb.ErrorCode, bool) {
	if syncErr, ok := errors.AsType[*SyncError](err); ok {
		return syncErr.Code, true
	}
	return pb.ErrorCode_ERROR_CODE_UNSPECIFIED, false
}
