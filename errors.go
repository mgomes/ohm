package ohm

import (
	"errors"
	"net/http"

	"github.com/go-chi/render"
)

// HTTPError is an error with an HTTP response status.
type HTTPError struct {
	Status  int
	Message string
	Err     error
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

// NewHTTPError creates an HTTPError.
func NewHTTPError(status int, message string, err error) *HTTPError {
	return &HTTPError{
		Status:  status,
		Message: message,
		Err:     err,
	}
}

// DefaultErrorHandler renders handler errors as plain text through chi/render.
func DefaultErrorHandler(req *Request, err error) {
	status := http.StatusInternalServerError
	message := http.StatusText(status)

	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		status = httpErr.responseStatus()
		message = http.StatusText(status)
		if httpErr.Message != "" {
			message = httpErr.Message
		}
	}

	render.Status(req.HTTPRequest(), status)
	render.PlainText(req.ResponseWriter(), req.HTTPRequest(), message)
}

func (e *HTTPError) responseStatus() int {
	if e.Status >= 100 && e.Status <= 999 {
		return e.Status
	}
	return http.StatusInternalServerError
}
