package ohm

import (
	"errors"
	"net/http"
)

// HTTPError is an error with an HTTP response status.
type HTTPError struct {
	Status  int
	Message string
	Err     error
}

// DecodeError is an error decoding malformed client request input.
type DecodeError struct {
	Status int
	Err    error
}

// Error returns the error message.
func (e *HTTPError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return http.StatusText(e.responseStatus())
}

// Unwrap returns the underlying error.
func (e *HTTPError) Unwrap() error {
	return e.Err
}

// Error returns the decode error message.
func (e *DecodeError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return http.StatusText(e.responseStatus())
}

// Unwrap returns the underlying decode error.
func (e *DecodeError) Unwrap() error {
	return e.Err
}

// NewHTTPError creates an HTTPError.
func NewHTTPError(status int, message string, err error) *HTTPError {
	return &HTTPError{
		Status:  status,
		Message: message,
		Err:     err,
	}
}

// DefaultErrorHandler renders handler errors as plain text.
func DefaultErrorHandler(req *Request, err error) {
	status, message := ErrorResponse(err)
	req.PlainText(status, message)
}

// ErrorResponse returns the safe HTTP status and public message for err.
func ErrorResponse(err error) (int, string) {
	status := http.StatusInternalServerError
	message := http.StatusText(status)

	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		status = httpErr.responseStatus()
		message = http.StatusText(status)
		if httpErr.Message != "" {
			message = httpErr.Message
		}
		return status, message
	}

	var decodeErr *DecodeError
	if errors.As(err, &decodeErr) {
		status = decodeErr.responseStatus()
		message = http.StatusText(status)
	}

	return status, message
}

func (e *HTTPError) responseStatus() int {
	if e.Status >= 100 && e.Status <= 999 {
		return e.Status
	}
	return http.StatusInternalServerError
}

func (e *DecodeError) responseStatus() int {
	if e.Status >= 400 && e.Status <= 499 {
		return e.Status
	}
	return http.StatusBadRequest
}
