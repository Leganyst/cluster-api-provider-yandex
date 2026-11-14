package ycerrors

import (
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// IsNotFound reports whether err is a YandexCloud API error
// with GRPC NOT_FOUND code.
func IsNotFound(err error) bool {
	if err == nil {
		return false
	}

	s, ok := status.FromError(err)
	return ok && s.Code() == codes.NotFound
}

// Message returns a text for err that is suitable for a condition message.
//
// YandexCloud SDK errors carry a per-request identifier in their text, so the same
// failure produces a different string on every call. A condition message that changes
// on every reconcile makes the object change on every reconcile, which in turn triggers
// the next reconcile immediately. For GRPC errors only the stable parts, the status code
// and the status message, are returned.
func Message(err error) string {
	if err == nil {
		return ""
	}

	if s, ok := status.FromError(err); ok && s.Code() != codes.OK {
		return fmt.Sprintf("%s: %s", s.Code(), s.Message())
	}

	return err.Error()
}
