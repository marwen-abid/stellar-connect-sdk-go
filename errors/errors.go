// Package errors defines the error taxonomy for the StellarConnect SDK.
//
// All SDK errors are represented as StellarConnectError, which provides:
//   - Code: Machine-readable error identifier
//   - Message: Human-readable error description
//   - Layer: Which component layer produced the error (core, anchor, client, observer)
//   - Cause: Underlying error, if any
//   - Context: Additional error details (asset name, account address, etc.)
//
// Error codes are organized by layer and severity, matching the RFC Section 9 taxonomy.
// Use the provided constructor functions (NewCoreError, NewAnchorError, etc.)
// to create properly typed errors with automatic layer assignment.
package errors

import "fmt"

// Code is a machine-readable error identifier.
type Code string

// Error codes - Core Layer
const (
	TOML_FETCH_FAILED         Code = "TOML_FETCH_FAILED"
	TOML_INVALID              Code = "TOML_INVALID"
	TOML_SIGNING_KEY_MISMATCH Code = "TOML_SIGNING_KEY_MISMATCH"
	NETWORK_ERROR             Code = "NETWORK_ERROR"
	ACCOUNT_NOT_FOUND         Code = "ACCOUNT_NOT_FOUND"
)

// Error codes - Anchor Layer
const (
	CONFIG_INVALID            Code = "CONFIG_INVALID"
	CHALLENGE_BUILD_FAILED    Code = "CHALLENGE_BUILD_FAILED"
	CHALLENGE_VERIFY_FAILED   Code = "CHALLENGE_VERIFY_FAILED"
	JWT_ISSUE_FAILED          Code = "JWT_ISSUE_FAILED"
	JWT_VERIFICATION_FAILED   Code = "JWT_VERIFICATION_FAILED"
	STORE_ERROR               Code = "STORE_ERROR"
	INVALID_ASSET             Code = "INVALID_ASSET"
	TRANSITION_INVALID        Code = "TRANSITION_INVALID"
	INTERACTIVE_TOKEN_INVALID Code = "INTERACTIVE_TOKEN_INVALID"
	PAYMENT_MISMATCH          Code = "PAYMENT_MISMATCH"
)

// Error codes - Client Layer
const (
	SIGNER_ERROR                Code = "SIGNER_ERROR"
	SIGNER_TIMEOUT              Code = "SIGNER_TIMEOUT"
	AUTH_UNSUPPORTED            Code = "AUTH_UNSUPPORTED"
	CHALLENGE_FETCH_FAILED      Code = "CHALLENGE_FETCH_FAILED"
	CHALLENGE_INVALID           Code = "CHALLENGE_INVALID"
	AUTH_REJECTED               Code = "AUTH_REJECTED"
	JWT_EXPIRED                 Code = "JWT_EXPIRED"
	TRANSFER_INIT_FAILED        Code = "TRANSFER_INIT_FAILED"
	TRANSFER_STATUS_POLL_FAILED Code = "TRANSFER_STATUS_POLL_FAILED"
	ROUTE_UNAVAILABLE           Code = "ROUTE_UNAVAILABLE"
)

// Error codes - Observer Layer
const (
	STREAM_ERROR        Code = "STREAM_ERROR"
	STREAM_DISCONNECTED Code = "STREAM_DISCONNECTED"
	CURSOR_SAVE_FAILED  Code = "CURSOR_SAVE_FAILED"
	HANDLER_PANIC       Code = "HANDLER_PANIC"
)

// StellarConnectError is the base error type for all SDK errors.
type StellarConnectError struct {
	Code    Code
	Message string
	Layer   string // "core", "anchor", "client", "observer"
	Cause   error
	Context map[string]any
}

// Error returns a formatted error string.
func (e *StellarConnectError) Error() string {
	msg := fmt.Sprintf("[%s] %s: %s", e.Layer, e.Code, e.Message)
	if e.Cause != nil {
		msg += fmt.Sprintf(" (caused by: %v)", e.Cause)
	}
	return msg
}

// Unwrap returns the underlying cause error, enabling error chain inspection.
func (e *StellarConnectError) Unwrap() error {
	return e.Cause
}

// NewCoreError creates a core layer error.
func NewCoreError(code Code, message string, cause error) *StellarConnectError {
	return &StellarConnectError{
		Code:    code,
		Message: message,
		Layer:   "core",
		Cause:   cause,
		Context: make(map[string]any),
	}
}

// NewAnchorError creates an anchor layer error.
func NewAnchorError(code Code, message string, cause error) *StellarConnectError {
	return &StellarConnectError{
		Code:    code,
		Message: message,
		Layer:   "anchor",
		Cause:   cause,
		Context: make(map[string]any),
	}
}

// NewClientError creates a client layer error.
func NewClientError(code Code, message string, cause error) *StellarConnectError {
	return &StellarConnectError{
		Code:    code,
		Message: message,
		Layer:   "client",
		Cause:   cause,
		Context: make(map[string]any),
	}
}

// NewObserverError creates an observer layer error.
func NewObserverError(code Code, message string, cause error) *StellarConnectError {
	return &StellarConnectError{
		Code:    code,
		Message: message,
		Layer:   "observer",
		Cause:   cause,
		Context: make(map[string]any),
	}
}

// Is checks if the target error is a StellarConnectError with the same code.
func (e *StellarConnectError) Is(target error) bool {
	if target == nil {
		return false
	}
	other, ok := target.(*StellarConnectError)
	if !ok {
		return false
	}
	return e.Code == other.Code
}

// As checks if target is a StellarConnectError and assigns it.
func As(err error, target **StellarConnectError) bool {
	if err == nil {
		return false
	}
	if v, ok := err.(*StellarConnectError); ok {
		*target = v
		return true
	}
	return false
}
