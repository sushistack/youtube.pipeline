package testutil

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
)

// AssertEqual fails the test if got != want, reporting a clear diff with file/line.
func AssertEqual[T comparable](t testing.TB, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

// AssertJSONEq fails the test if the two JSON strings are not semantically equal.
func AssertJSONEq(t testing.TB, got, want string) {
	t.Helper()
	var gotVal, wantVal any
	if err := json.Unmarshal([]byte(got), &gotVal); err != nil {
		t.Fatalf("unmarshal got: %v", err)
	}
	if err := json.Unmarshal([]byte(want), &wantVal); err != nil {
		t.Fatalf("unmarshal want: %v", err)
	}
	if !reflect.DeepEqual(gotVal, wantVal) {
		t.Errorf("JSON mismatch:\ngot:  %s\nwant: %s", got, want)
	}
}

// AssertFloatNear fails the test if got and want differ by more than eps.
func AssertFloatNear(t testing.TB, got, want, eps float64) {
	t.Helper()
	if math.Abs(got-want) > eps {
		t.Errorf("got %0.12f, want %0.12f (eps=%0.12f)", got, want, eps)
	}
}
