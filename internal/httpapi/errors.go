package httpapi

import (
	"errors"
	"fmt"
	"net/http"
)

type errorCode string

const (
	codeBadRequest        errorCode = "bad_request"
	codeUnauthorized      errorCode = "unauthorized"
	codeForbidden         errorCode = "forbidden"
	codeNotFound          errorCode = "not_found"
	codeMethodNotAllowed  errorCode = "method_not_allowed"
	codeConflict          errorCode = "conflict"
	codeRateLimitExceeded errorCode = "rate_limit_exceeded"
	codeInternalError     errorCode = "internal_error"
	codeDeadlineExceeded  errorCode = "deadline_exceeded"
)

type errorBody struct {
	Code      errorCode `json:"code"`
	Message   string    `json:"message"`
	RequestID string    `json:"request_id"`
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

var (
	ErrUnsupportedMediaType  = errors.New("unsupported media type")
	ErrEmptyBody             = errors.New("empty body")
	ErrBodyTooLarge          = errors.New("body too large")
	ErrUnknownJSONField      = errors.New("unknown json field")
	ErrMalformedJSON         = errors.New("malformed json")
	ErrInvalidJSONType       = errors.New("invalid json type")
	ErrMultipleJSONValues    = errors.New("multiple json values")
	ErrNotAuthenticated      = errors.New("not authenticated")
	ErrNilRequestIDGenerator = errors.New("request ID generator must not be nil")
	ErrNilLogger             = errors.New("logger must not be nil")
	ErrInvalidTimeout        = errors.New("timeout must be > 0")
	ErrNilRateLimiter        = errors.New("rate limiter must not be nil")
	ErrNilAuthenticator      = errors.New("authenticator must not be nil")
	ErrNoAllowedRoles        = errors.New("allowed roles list must not be empty")
	ErrInvalidAllowedRole    = errors.New("unknown allowed role")
	ErrInvalidErrorCode      = errors.New("invalid error code")
	ErrNilIdentityService    = errors.New("identity service must not be nil")
	ErrNilPublicSaleService  = errors.New("public sale service must not be nil")
	ErrInvalidPagination     = errors.New("invalid pagination parameters")
	ErrNilAdminSaleService   = errors.New("admin sale service must not be nil")
	ErrNilNewIDFunc          = errors.New("new id func must not be nil")
	ErrNilNowFunc            = errors.New("now func must not be nil")
	ErrInvalidSaleID         = errors.New("invalid sale id")
)

func (code errorCode) valid() bool {
	switch code {
	case codeBadRequest,
		codeUnauthorized,
		codeForbidden,
		codeNotFound,
		codeMethodNotAllowed,
		codeConflict,
		codeRateLimitExceeded,
		codeInternalError,
		codeDeadlineExceeded:
		return true

	default:
		return false
	}
}

func writeError(
	w http.ResponseWriter,
	status int,
	code errorCode,
	message string,
	requestID string,
) error {
	if !code.valid() {
		return fmt.Errorf(
			"invalid code error: %q: %w",
			code,
			ErrInvalidErrorCode,
		)
	}

	err := errorEnvelope{
		Error: errorBody{
			Code:      code,
			Message:   message,
			RequestID: requestID,
		},
	}

	return writeJson(w, status, err)
}
