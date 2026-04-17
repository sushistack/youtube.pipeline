package domain

import "errors"

// DomainError represents a classified application error with HTTP status
// and retry semantics.
type DomainError struct {
	Code       string
	Message    string
	HTTPStatus int
	Retryable  bool
}

func (e *DomainError) Error() string { return e.Message }

var (
	ErrRateLimited     = &DomainError{Code: "RATE_LIMITED", Message: "rate limited", HTTPStatus: 429, Retryable: true}
	ErrUpstreamTimeout = &DomainError{Code: "UPSTREAM_TIMEOUT", Message: "upstream timeout", HTTPStatus: 504, Retryable: true}
	ErrStageFailed     = &DomainError{Code: "STAGE_FAILED", Message: "stage failed", HTTPStatus: 500, Retryable: true}
	ErrValidation      = &DomainError{Code: "VALIDATION_ERROR", Message: "validation error", HTTPStatus: 400, Retryable: false}
	ErrConflict        = &DomainError{Code: "CONFLICT", Message: "conflict", HTTPStatus: 409, Retryable: false}
	ErrCostCapExceeded = &DomainError{Code: "COST_CAP_EXCEEDED", Message: "cost cap exceeded", HTTPStatus: 402, Retryable: false}
	ErrNotFound        = &DomainError{Code: "NOT_FOUND", Message: "not found", HTTPStatus: 404, Retryable: false}
)

// Classify extracts DomainError attributes from any error chain.
// Returns 500/INTERNAL_ERROR/false for unclassified errors.
func Classify(err error) (httpStatus int, code string, retryable bool) {
	var de *DomainError
	if errors.As(err, &de) {
		return de.HTTPStatus, de.Code, de.Retryable
	}
	return 500, "INTERNAL_ERROR", false
}
