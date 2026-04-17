package testutil

import (
	"fmt"
	"strings"
	"testing"
)

// fakeT captures calls to Errorf / Fatalf for testing assertion helpers.
type fakeT struct {
	testing.TB
	helperCalled bool
	errors       []string
	fatals       []string
}

func (f *fakeT) Helper()                        { f.helperCalled = true }
func (f *fakeT) Errorf(format string, args ...any) { f.errors = append(f.errors, fmt.Sprintf(format, args...)) }
func (f *fakeT) Fatalf(format string, args ...any) { f.fatals = append(f.fatals, fmt.Sprintf(format, args...)) }

func TestAssertEqual_Pass(t *testing.T) {
	ft := &fakeT{}
	AssertEqual(ft, 42, 42)
	if len(ft.errors) != 0 {
		t.Errorf("expected no errors, got %v", ft.errors)
	}
}

func TestAssertEqual_Fail(t *testing.T) {
	ft := &fakeT{}
	AssertEqual(ft, 1, 2)
	if !ft.helperCalled {
		t.Error("expected t.Helper() to be called")
	}
	if len(ft.errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(ft.errors))
	}
	if !strings.Contains(ft.errors[0], "got 1") || !strings.Contains(ft.errors[0], "want 2") {
		t.Errorf("unexpected error message: %s", ft.errors[0])
	}
}

func TestAssertEqual_Strings(t *testing.T) {
	ft := &fakeT{}
	AssertEqual(ft, "hello", "hello")
	if len(ft.errors) != 0 {
		t.Errorf("expected no errors for equal strings")
	}

	ft2 := &fakeT{}
	AssertEqual(ft2, "hello", "world")
	if len(ft2.errors) != 1 {
		t.Errorf("expected 1 error for unequal strings, got %d", len(ft2.errors))
	}
}

func TestAssertJSONEq_Pass(t *testing.T) {
	ft := &fakeT{}
	AssertJSONEq(ft, `{"a":1,"b":2}`, `{"b":2,"a":1}`)
	if len(ft.errors) != 0 {
		t.Errorf("expected no errors for semantically equal JSON")
	}
}

func TestAssertJSONEq_Fail(t *testing.T) {
	ft := &fakeT{}
	AssertJSONEq(ft, `{"a":1}`, `{"a":2}`)
	if len(ft.errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(ft.errors))
	}
	if !strings.Contains(ft.errors[0], "JSON mismatch") {
		t.Errorf("unexpected error message: %s", ft.errors[0])
	}
}

func TestAssertJSONEq_InvalidJSON(t *testing.T) {
	ft := &fakeT{}
	AssertJSONEq(ft, `not json`, `{"a":1}`)
	if len(ft.fatals) != 1 {
		t.Fatalf("expected 1 fatal for invalid JSON, got %d", len(ft.fatals))
	}
}
