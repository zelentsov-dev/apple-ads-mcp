package tools

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"

	"github.com/zelentsov-dev/apple-ads-mcp/internal/appleads"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/operations"
)

type classifiedError struct {
	kind      string
	code      string
	message   string
	hint      string
	retryable bool
	details   map[string]any
	cause     error
}

func (e *classifiedError) Error() string {
	return e.message
}

func (e *classifiedError) Unwrap() error {
	return e.cause
}

func writeGateError(code, message, hint string) error {
	return &classifiedError{kind: "write_gate_error", code: code, message: message, hint: hint}
}

func classifyError(err error) (output ErrorOutput) {
	defer func() {
		if output.Details == nil {
			output.Details = map[string]any{}
		}
	}()

	var classified *classifiedError
	if errors.As(err, &classified) {
		return ErrorOutput{
			Type: classified.kind, Message: classified.message, Code: classified.code,
			Retryable: classified.retryable, Details: classified.details, Hint: classified.hint,
		}
	}

	var ambiguous *appleads.AmbiguousWriteError
	if errors.As(err, &ambiguous) {
		return ErrorOutput{
			Type: "ambiguous_write", Message: "write outcome is unknown and must be verified",
			Code: "AMBIGUOUS_WRITE", Retryable: false,
			Hint: "do not repeat the write; call operations_verify and read every target directly",
		}
	}

	var apiError *appleads.APIError
	if errors.As(err, &apiError) {
		return ErrorOutput{
			Type: "apple_api_error", Message: apiError.Message, HTTPStatus: apiError.HTTPStatus,
			Code: apiError.Code, ResponseFormat: apiError.ResponseFormat, Retryable: apiError.Retryable,
			Details: apiError.Details, AppleBody: apiError.Body, Hint: appleAPIHint(apiError),
		}
	}

	switch {
	case errors.Is(err, operations.ErrReceiptExpired):
		return ErrorOutput{Type: "receipt_expired", Message: operations.ErrReceiptExpired.Error(), Code: "RECEIPT_EXPIRED", Hint: "create a new preview and apply its new receipt"}
	case errors.Is(err, operations.ErrReceiptUsed):
		return ErrorOutput{Type: "receipt_used", Message: operations.ErrReceiptUsed.Error(), Code: "RECEIPT_USED", Hint: "inspect or verify the original operation; never apply the receipt again"}
	case errors.Is(err, operations.ErrReceiptNotFound):
		return ErrorOutput{Type: "receipt_not_found", Message: operations.ErrReceiptNotFound.Error(), Code: "RECEIPT_NOT_FOUND", Hint: "use the exact receipt returned by this running server"}
	case errors.Is(err, operations.ErrStateDrift):
		return ErrorOutput{Type: "state_drift", Message: operations.ErrStateDrift.Error(), Code: "STATE_DRIFT", Hint: "discard this receipt, inspect current state, and create a new preview if still authorized"}
	case isTransportError(err):
		return ErrorOutput{Type: "transport_error", Message: err.Error(), Code: "TRANSPORT_ERROR", Retryable: true, Hint: "check connectivity and retry only read operations; never repeat an ambiguous write"}
	default:
		return ErrorOutput{Type: "validation_error", Message: err.Error(), Code: "VALIDATION_ERROR", Hint: "correct the tool arguments and retry"}
	}
}

func appleAPIHint(err *appleads.APIError) string {
	switch err.HTTPStatus {
	case 401:
		return "refresh Apple Ads credentials and verify the selected profile"
	case 403:
		return "verify the selected ad account and Apple Ads API role"
	case 429:
		return "honor Apple rate-limit timing before retrying the read"
	default:
		if err.HTTPStatus >= 400 && err.HTTPStatus < 500 {
			return "correct the request using the Apple error details; do not retry it unchanged"
		}
		if err.HTTPStatus >= 500 {
			return "Apple returned a server error; retry only reads and do not repeat an ambiguous write"
		}
		return "inspect the Apple error details before retrying"
	}
}

func isTransportError(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netError net.Error
	if errors.As(err, &netError) {
		return true
	}
	var urlError *url.Error
	if errors.As(err, &urlError) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "execute apple ads request") || strings.Contains(message, "request access token")
}
