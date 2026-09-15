package pkg

import "fmt"

// Exit codes per spec §11.1.
const (
	ExitOK             = 0
	ExitError          = 1 // generic error
	ExitAuth           = 2 // authentication error
	ExitModuleNotFound = 3 // module not found
	ExitManifest       = 4 // invalid manifest
	ExitNetwork        = 5 // network error
	ExitPermission     = 6 // permission error
	ExitMFA            = 7 // MFA required / failed
	ExitBuild          = 10
	ExitPublish        = 11
	ExitSigning        = 12
)

// Error is a categorized CLI error rendered as
// `✗ <Category> : <Message>` + `→ <Fix>`.
type Error struct {
	Category string
	Message  string
	Fix      string
	Code     int
}

var _ error = (*Error)(nil)

func (e *Error) Error() string {
	return e.Message
}

// ExitCode returns the process exit code associated with the error.
func (e *Error) ExitCode() int {
	if e.Code == 0 {
		return ExitError
	}
	return e.Code
}

// NewError builds a categorized error.
func NewError(category, message string, code int) *Error {
	return &Error{Category: category, Message: message, Code: code}
}

// NewErrorWithFix builds a categorized error with a corrective action hint.
func NewErrorWithFix(category, message, fix string, code int) *Error {
	return &Error{Category: category, Message: message, Fix: fix, Code: code}
}

// ExitCodeFor returns the exit code implied by an arbitrary error, defaulting
// to generic error.
func ExitCodeFor(err error) int {
	if e, ok := err.(*Error); ok {
		return e.ExitCode()
	}
	return ExitError
}

// FormatError renders an error in the `✗ Category : Message` format.
func FormatError(err error) string {
	if e, ok := err.(*Error); ok {
		out := fmt.Sprintf("✗ %s : %s", e.Category, e.Message)
		if e.Fix != "" {
			out += fmt.Sprintf("\n  → %s", e.Fix)
		}
		return out
	}
	return fmt.Sprintf("✗ Error: %s", err)
}
