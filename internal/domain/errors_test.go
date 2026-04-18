package domain

import (
	"errors"
	"fmt"
	"testing"
)

func TestDomainErrors_Classification(t *testing.T) {
	tests := []struct {
		err       *DomainError
		wantHTTP  int
		wantRetry bool
	}{
		{ErrRateLimited, 429, true},
		{ErrUpstreamTimeout, 504, true},
		{ErrStageFailed, 500, true},
		{ErrValidation, 400, false},
		{ErrConflict, 409, false},
		{ErrCostCapExceeded, 402, false},
		{ErrNotFound, 404, false},
		{ErrAntiProgress, 422, false},
	}
	for _, tt := range tests {
		t.Run(tt.err.Code, func(t *testing.T) {
			if tt.err.HTTPStatus != tt.wantHTTP {
				t.Errorf("HTTPStatus = %d, want %d", tt.err.HTTPStatus, tt.wantHTTP)
			}
			if tt.err.Retryable != tt.wantRetry {
				t.Errorf("Retryable = %v, want %v", tt.err.Retryable, tt.wantRetry)
			}
		})
	}
}

func TestDomainErrors_Count(t *testing.T) {
	allErrors := []*DomainError{
		ErrRateLimited, ErrUpstreamTimeout, ErrStageFailed,
		ErrValidation, ErrConflict, ErrCostCapExceeded, ErrNotFound,
		ErrAntiProgress,
	}
	if got := len(allErrors); got != 8 {
		t.Errorf("sentinel error count = %d, want 8", got)
	}
}

func TestDomainErrors_ErrorsIs(t *testing.T) {
	wrapped := fmt.Errorf("stage write: %w", ErrStageFailed)
	if !errors.Is(wrapped, ErrStageFailed) {
		t.Error("errors.Is failed to unwrap ErrStageFailed")
	}
	if errors.Is(wrapped, ErrNotFound) {
		t.Error("errors.Is falsely matched ErrNotFound")
	}
}

func TestDomainErrors_ErrorsAs(t *testing.T) {
	wrapped := fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", ErrRateLimited))
	var de *DomainError
	if !errors.As(wrapped, &de) {
		t.Fatal("errors.As failed to extract DomainError")
	}
	if de.Code != "RATE_LIMITED" {
		t.Errorf("Code = %q, want RATE_LIMITED", de.Code)
	}
}

func TestClassify_DomainError(t *testing.T) {
	wrapped := fmt.Errorf("context: %w", ErrCostCapExceeded)
	status, code, retryable := Classify(wrapped)
	if status != 402 {
		t.Errorf("status = %d, want 402", status)
	}
	if code != "COST_CAP_EXCEEDED" {
		t.Errorf("code = %q, want COST_CAP_EXCEEDED", code)
	}
	if retryable {
		t.Error("retryable = true, want false")
	}
}

func TestClassify_AntiProgress(t *testing.T) {
	status, code, retryable := Classify(ErrAntiProgress)
	if status != 422 {
		t.Errorf("status = %d, want 422", status)
	}
	if code != "ANTI_PROGRESS" {
		t.Errorf("code = %q, want ANTI_PROGRESS", code)
	}
	if retryable {
		t.Error("retryable = true, want false")
	}
	if ErrAntiProgress.Message != "Retries producing similar output — human review required" {
		t.Errorf("Message = %q, want exact operator-facing text", ErrAntiProgress.Message)
	}
}

func TestClassify_AntiProgress_WrappedError(t *testing.T) {
	err := fmt.Errorf("stage write: %w", ErrAntiProgress)
	status, code, retryable := Classify(err)
	if status != 422 || code != "ANTI_PROGRESS" || retryable {
		t.Errorf("Classify(wrapped) = (%d, %q, %v), want (422, ANTI_PROGRESS, false)", status, code, retryable)
	}
	if !errors.Is(err, ErrAntiProgress) {
		t.Error("errors.Is failed to unwrap ErrAntiProgress")
	}
}

func TestClassify_UnknownError(t *testing.T) {
	status, code, retryable := Classify(fmt.Errorf("random error"))
	if status != 500 {
		t.Errorf("status = %d, want 500", status)
	}
	if code != "INTERNAL_ERROR" {
		t.Errorf("code = %q, want INTERNAL_ERROR", code)
	}
	if retryable {
		t.Error("retryable = true, want false")
	}
}
