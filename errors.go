package wau

import (
	"errors"
	"fmt"
)

// APIError represents an error returned by the WAU kernel.
type APIError struct {
	StatusCode int
	Code       string // optional, parsed from response body "code" field
	Message    string
	RequestID  string // optional X-Request-ID from response
	Body       []byte // raw response body for debugging
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("wau: %s (status %d, code=%s, request_id=%s): %s",
			e.Message, e.StatusCode, e.Code, e.RequestID, string(e.Body))
	}
	return fmt.Sprintf("wau: %s (status %d, request_id=%s): %s",
		e.Message, e.StatusCode, e.RequestID, string(e.Body))
}

// Is enables errors.Is(err, ErrNotFound) etc.
func (e *APIError) Is(target error) bool {
	var t *APIError
	if !errors.As(target, &t) {
		return false
	}
	return e.StatusCode == t.StatusCode
}

// Sentinel errors for typed error matching.
var (
	ErrNotFound       = &APIError{StatusCode: 404, Code: "not_found", Message: "resource not found"}
	ErrUnauthorized   = &APIError{StatusCode: 401, Code: "unauthorized", Message: "unauthorized"}
	ErrForbidden      = &APIError{StatusCode: 403, Code: "forbidden", Message: "forbidden"}
	ErrBadRequest     = &APIError{StatusCode: 400, Code: "bad_request", Message: "bad request"}
	ErrConflict       = &APIError{StatusCode: 409, Code: "conflict", Message: "conflict"}
	ErrInternal       = &APIError{StatusCode: 500, Code: "internal", Message: "internal server error"}
	ErrCircuitOpen    = errors.New("wau: circuit breaker is open")
	ErrMaxRetries     = errors.New("wau: max retries exceeded")
	ErrNotImplemented = errors.New("wau: not implemented in this preview")
)
