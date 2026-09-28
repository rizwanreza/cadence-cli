package cli

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/rizwanreza/cadence-cli/internal/api"
)

// Exit codes. Scripts and agents branch on these; keep them stable.
const (
	ExitOK       = 0 // success
	ExitError    = 1 // anything else (server error, rate limited, doctor failure)
	ExitUsage    = 2 // bad flags, missing arguments, unknown command, 400
	ExitAuth     = 3 // not signed in, token rejected (401/403)
	ExitNotFound = 4 // goal, cycle or week not found (404)
	ExitConflict = 5 // conflict or validation failure (409/422)
	ExitNetwork  = 6 // host unreachable or timed out
)

// UsageError is a mistake in how the command was invoked (exit 2).
type UsageError struct {
	Msg  string
	Hint string
}

func (e *UsageError) Error() string { return e.Msg }

func usageErrorf(format string, args ...any) error {
	return &UsageError{Msg: fmt.Sprintf(format, args...)}
}

// errSilent means the command already reported its failure (e.g. doctor).
var errSilent = errors.New("silent failure")

// exitError carries an explicit exit code with a message.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

// ExitCode maps an error to the process exit code.
func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	var exit *exitError
	if errors.As(err, &exit) {
		return exit.code
	}
	var usage *UsageError
	if errors.As(err, &usage) {
		return ExitUsage
	}
	var netErr *api.NetworkError
	if errors.As(err, &netErr) {
		return ExitNetwork
	}
	var apiErr *api.Error
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case http.StatusBadRequest:
			return ExitUsage
		case http.StatusUnauthorized, http.StatusForbidden:
			return ExitAuth
		case http.StatusNotFound:
			return ExitNotFound
		case http.StatusConflict, http.StatusUnprocessableEntity:
			return ExitConflict
		}
		return ExitError
	}
	var notFound *notFoundError
	if errors.As(err, &notFound) {
		return ExitNotFound
	}
	return ExitError
}

// errorCode is the machine code printed in JSON errors.
func errorCode(err error) string {
	var usage *UsageError
	var netErr *api.NetworkError
	var apiErr *api.Error
	var notFound *notFoundError
	switch {
	case errors.As(err, &usage):
		return "usage"
	case errors.As(err, &netErr):
		return "network"
	case errors.As(err, &apiErr):
		return apiErr.Code
	case errors.As(err, &notFound):
		return "not_found"
	}
	switch ExitCode(err) {
	case ExitAuth:
		return "unauthorized"
	case ExitConflict:
		return "conflict"
	}
	return "error"
}

func errorDetails(err error) []string {
	var apiErr *api.Error
	if errors.As(err, &apiErr) && len(apiErr.Details) > 0 {
		return apiErr.Details
	}
	return []string{}
}

// notFoundError is a client-side lookup miss (e.g. no goal with that slug).
type notFoundError struct{ msg string }

func (e *notFoundError) Error() string { return e.msg }

func notFoundf(format string, args ...any) error {
	return &notFoundError{msg: fmt.Sprintf(format, args...)}
}
