package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// --- HumanRenderer tests ---

func TestHumanRenderer_RenderSuccess_DoctorAllPassed(t *testing.T) {
	var buf bytes.Buffer
	r := NewHumanRenderer(&buf)

	r.RenderSuccess(&DoctorOutput{
		Checks: []CheckResult{
			{Name: "API Keys", Passed: true},
			{Name: "FFmpeg", Passed: true},
		},
		Passed: true,
	})

	out := buf.String()

	if !strings.Contains(out, colorGreen) {
		t.Error("expected green ANSI code in passing output")
	}
	if !strings.Contains(out, colorReset) {
		t.Error("expected reset ANSI code after color")
	}
	if !strings.Contains(out, "\u2713") {
		t.Error("expected check mark for passing checks")
	}
	if !strings.Contains(out, "API Keys") {
		t.Error("expected check name in output")
	}
	if !strings.Contains(out, "2/2 checks passed") {
		t.Errorf("expected summary line, got: %s", out)
	}
}

func TestHumanRenderer_RenderSuccess_DoctorWithFailure(t *testing.T) {
	var buf bytes.Buffer
	r := NewHumanRenderer(&buf)

	r.RenderSuccess(&DoctorOutput{
		Checks: []CheckResult{
			{Name: "API Keys", Passed: true},
			{Name: "FFmpeg", Passed: false, Message: "ffmpeg not found"},
		},
		Passed: false,
	})

	out := buf.String()

	if !strings.Contains(out, colorRed) {
		t.Error("expected red ANSI code for failing check")
	}
	if !strings.Contains(out, "\u2717") {
		t.Error("expected cross mark for failing check")
	}
	if !strings.Contains(out, "ffmpeg not found") {
		t.Error("expected failure message in output")
	}
	if !strings.Contains(out, "1/2 checks passed") {
		t.Errorf("expected summary with 1/2 passed, got: %s", out)
	}
	if !strings.Contains(out, "fix failing checks") {
		t.Error("expected remediation hint")
	}
}

func TestHumanRenderer_RenderSuccess_InitOutput(t *testing.T) {
	var buf bytes.Buffer
	r := NewHumanRenderer(&buf)

	r.RenderSuccess(&InitOutput{
		Config:   "/tmp/config.yaml",
		Env:      "/tmp/.env",
		Database: "/tmp/pipeline.db",
		Output:   "/tmp/output",
	})

	out := buf.String()

	if !strings.Contains(out, colorGreen) {
		t.Error("expected green ANSI code in init output")
	}
	if !strings.Contains(out, "Initialized youtube.pipeline:") {
		t.Error("expected header line")
	}
	if !strings.Contains(out, "/tmp/config.yaml") {
		t.Error("expected config path")
	}
	if !strings.Contains(out, "/tmp/.env") {
		t.Error("expected env path")
	}
	if !strings.Contains(out, "/tmp/pipeline.db") {
		t.Error("expected database path")
	}
	if !strings.Contains(out, "/tmp/output") {
		t.Error("expected output path")
	}
}

func TestHumanRenderer_RenderSuccess_UnknownType(t *testing.T) {
	var buf bytes.Buffer
	r := NewHumanRenderer(&buf)

	r.RenderSuccess("plain string")

	out := buf.String()
	if !strings.Contains(out, "plain string") {
		t.Errorf("expected fallback to print value, got: %s", out)
	}
}

func TestHumanRenderer_RenderError(t *testing.T) {
	var buf bytes.Buffer
	r := NewHumanRenderer(&buf)

	r.RenderError(errors.New("something went wrong"))

	out := buf.String()

	if !strings.Contains(out, colorRed) {
		t.Error("expected red ANSI code in error output")
	}
	if !strings.Contains(out, colorReset) {
		t.Error("expected reset ANSI code after color")
	}
	if !strings.Contains(out, "something went wrong") {
		t.Errorf("expected error message, got: %s", out)
	}
}

// --- JSONRenderer tests ---

func TestJSONRenderer_RenderSuccess_DoctorOutput(t *testing.T) {
	var buf bytes.Buffer
	r := NewJSONRenderer(&buf)

	r.RenderSuccess(&DoctorOutput{
		Checks: []CheckResult{
			{Name: "API Keys", Passed: true},
			{Name: "FFmpeg", Passed: false, Message: "not found"},
		},
		Passed: false,
	})

	var env Envelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	testutil.AssertEqual(t, env.Version, 1)

	if env.Data == nil {
		t.Fatal("expected data field in envelope")
	}
	if env.Error != nil {
		t.Error("expected no error field in success envelope")
	}

	// Verify data structure via re-marshal round-trip.
	dataBytes, _ := json.Marshal(env.Data)
	var doc DoctorOutput
	if err := json.Unmarshal(dataBytes, &doc); err != nil {
		t.Fatalf("unmarshal doctor data: %v", err)
	}
	testutil.AssertEqual(t, len(doc.Checks), 2)
	testutil.AssertEqual(t, doc.Passed, false)
	testutil.AssertEqual(t, doc.Checks[0].Name, "API Keys")
	testutil.AssertEqual(t, doc.Checks[0].Passed, true)
	testutil.AssertEqual(t, doc.Checks[1].Name, "FFmpeg")
	testutil.AssertEqual(t, doc.Checks[1].Passed, false)
	testutil.AssertEqual(t, doc.Checks[1].Message, "not found")
}

func TestJSONRenderer_RenderSuccess_InitOutput(t *testing.T) {
	var buf bytes.Buffer
	r := NewJSONRenderer(&buf)

	r.RenderSuccess(&InitOutput{
		Config:   "/tmp/config.yaml",
		Env:      "/tmp/.env",
		Database: "/tmp/pipeline.db",
		Output:   "/tmp/output",
	})

	var env Envelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	testutil.AssertEqual(t, env.Version, 1)

	if env.Data == nil {
		t.Fatal("expected data in envelope")
	}

	dataBytes, _ := json.Marshal(env.Data)
	var init InitOutput
	if err := json.Unmarshal(dataBytes, &init); err != nil {
		t.Fatalf("unmarshal init data: %v", err)
	}
	testutil.AssertEqual(t, init.Config, "/tmp/config.yaml")
	testutil.AssertEqual(t, init.Env, "/tmp/.env")
	testutil.AssertEqual(t, init.Database, "/tmp/pipeline.db")
	testutil.AssertEqual(t, init.Output, "/tmp/output")
}

func TestJSONRenderer_RenderSuccess_JSONFieldsAreSnakeCase(t *testing.T) {
	var buf bytes.Buffer
	r := NewJSONRenderer(&buf)

	r.RenderSuccess(&DoctorOutput{
		Checks: []CheckResult{{Name: "test", Passed: true}},
		Passed: true,
	})

	raw := buf.String()

	// Verify snake_case fields, not camelCase.
	for _, field := range []string{`"version"`, `"data"`, `"checks"`, `"passed"`, `"name"`} {
		if !strings.Contains(raw, field) {
			t.Errorf("expected snake_case field %s in JSON output", field)
		}
	}
}

func TestJSONRenderer_RenderError_AllDomainErrors(t *testing.T) {
	tests := []struct {
		err         error
		wantCode    string
		wantRecover bool
	}{
		{domain.ErrRateLimited, "RATE_LIMITED", true},
		{domain.ErrUpstreamTimeout, "UPSTREAM_TIMEOUT", true},
		{domain.ErrStageFailed, "STAGE_FAILED", true},
		{domain.ErrValidation, "VALIDATION_ERROR", false},
		{domain.ErrConflict, "CONFLICT", false},
		{domain.ErrCostCapExceeded, "COST_CAP_EXCEEDED", false},
		{domain.ErrNotFound, "NOT_FOUND", false},
	}

	for _, tt := range tests {
		t.Run(tt.wantCode, func(t *testing.T) {
			var buf bytes.Buffer
			r := NewJSONRenderer(&buf)

			r.RenderError(tt.err)

			var env Envelope
			if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}

			testutil.AssertEqual(t, env.Version, 1)

			if env.Error == nil {
				t.Fatal("expected error in envelope")
			}
			if env.Data != nil {
				t.Error("expected no data in error envelope")
			}

			testutil.AssertEqual(t, env.Error.Code, tt.wantCode)
			testutil.AssertEqual(t, env.Error.Recoverable, tt.wantRecover)
			testutil.AssertEqual(t, env.Error.Message, tt.err.Error())
		})
	}
}

func TestJSONRenderer_RenderError_WrappedDomainError(t *testing.T) {
	wrapped := fmt.Errorf("context: %w", domain.ErrValidation)

	var buf bytes.Buffer
	r := NewJSONRenderer(&buf)

	r.RenderError(wrapped)

	var env Envelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	testutil.AssertEqual(t, env.Error.Code, "VALIDATION_ERROR")
	testutil.AssertEqual(t, env.Error.Recoverable, false)
}

func TestJSONRenderer_RenderError_InternalErrorFallback(t *testing.T) {
	var buf bytes.Buffer
	r := NewJSONRenderer(&buf)

	r.RenderError(errors.New("unknown problem"))

	var env Envelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	testutil.AssertEqual(t, env.Version, 1)

	if env.Error == nil {
		t.Fatal("expected error in envelope")
	}

	testutil.AssertEqual(t, env.Error.Code, "INTERNAL_ERROR")
	testutil.AssertEqual(t, env.Error.Recoverable, false)
	testutil.AssertEqual(t, env.Error.Message, "unknown problem")
}

func TestJSONRenderer_RenderError_JSONFieldsAreSnakeCase(t *testing.T) {
	var buf bytes.Buffer
	r := NewJSONRenderer(&buf)

	r.RenderError(errors.New("test"))

	raw := buf.String()

	for _, field := range []string{`"version"`, `"error"`, `"code"`, `"message"`, `"recoverable"`} {
		if !strings.Contains(raw, field) {
			t.Errorf("expected snake_case field %s in JSON error output", field)
		}
	}
}

// --- Round-trip tests ---

func TestJSONRenderer_RoundTrip_DoctorOutput(t *testing.T) {
	original := &DoctorOutput{
		Checks: []CheckResult{
			{Name: "API Keys", Passed: true},
			{Name: "FFmpeg", Passed: false, Message: "not found in PATH"},
		},
		Passed: false,
	}

	var buf bytes.Buffer
	r := NewJSONRenderer(&buf)
	r.RenderSuccess(original)

	// Parse the full envelope.
	var env struct {
		Version int          `json:"version"`
		Data    DoctorOutput `json:"data"`
	}
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	testutil.AssertEqual(t, env.Version, 1)
	testutil.AssertEqual(t, env.Data.Passed, false)
	testutil.AssertEqual(t, len(env.Data.Checks), 2)
	testutil.AssertEqual(t, env.Data.Checks[0].Name, "API Keys")
	testutil.AssertEqual(t, env.Data.Checks[0].Passed, true)
	testutil.AssertEqual(t, env.Data.Checks[1].Name, "FFmpeg")
	testutil.AssertEqual(t, env.Data.Checks[1].Passed, false)
	testutil.AssertEqual(t, env.Data.Checks[1].Message, "not found in PATH")
}

func TestJSONRenderer_RoundTrip_InitOutput(t *testing.T) {
	original := &InitOutput{
		Config:   "/home/user/.youtube-pipeline/config.yaml",
		Env:      "/home/user/.youtube-pipeline/.env",
		Database: "/home/user/.youtube-pipeline/pipeline.db",
		Output:   "/home/user/.youtube-pipeline/output",
	}

	var buf bytes.Buffer
	r := NewJSONRenderer(&buf)
	r.RenderSuccess(original)

	var env struct {
		Version int        `json:"version"`
		Data    InitOutput `json:"data"`
	}
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	testutil.AssertEqual(t, env.Version, 1)
	testutil.AssertEqual(t, env.Data.Config, original.Config)
	testutil.AssertEqual(t, env.Data.Env, original.Env)
	testutil.AssertEqual(t, env.Data.Database, original.Database)
	testutil.AssertEqual(t, env.Data.Output, original.Output)
}

func TestJSONRenderer_RoundTrip_ErrorEnvelope(t *testing.T) {
	var buf bytes.Buffer
	r := NewJSONRenderer(&buf)
	r.RenderError(domain.ErrRateLimited)

	var env struct {
		Version int       `json:"version"`
		Error   ErrorInfo `json:"error"`
	}
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	testutil.AssertEqual(t, env.Version, 1)
	testutil.AssertEqual(t, env.Error.Code, "RATE_LIMITED")
	testutil.AssertEqual(t, env.Error.Message, "rate limited")
	testutil.AssertEqual(t, env.Error.Recoverable, true)
}

// --- Semantic JSON equality test ---

func TestJSONRenderer_SemanticEquality(t *testing.T) {
	var buf bytes.Buffer
	r := NewJSONRenderer(&buf)

	r.RenderSuccess(&InitOutput{
		Config:   "/a/config.yaml",
		Env:      "/a/.env",
		Database: "/a/db",
		Output:   "/a/out",
	})

	want := `{"version":1,"data":{"config":"/a/config.yaml","env":"/a/.env","database":"/a/db","output":"/a/out"}}`
	testutil.AssertJSONEq(t, strings.TrimSpace(buf.String()), want)
}

// --- Interface compliance ---

func TestRendererInterfaceCompliance(t *testing.T) {
	var _ Renderer = NewHumanRenderer(&bytes.Buffer{})
	var _ Renderer = NewJSONRenderer(&bytes.Buffer{})
}

// --- newRenderer helper ---

func TestNewRenderer_JSON(t *testing.T) {
	jsonOutput = true
	defer func() { jsonOutput = false }()

	var buf bytes.Buffer
	r := newRenderer(&buf)

	if _, ok := r.(*JSONRenderer); !ok {
		t.Error("expected JSONRenderer when jsonOutput is true")
	}
}

func TestNewRenderer_Human(t *testing.T) {
	jsonOutput = false

	var buf bytes.Buffer
	r := newRenderer(&buf)

	if _, ok := r.(*HumanRenderer); !ok {
		t.Error("expected HumanRenderer when jsonOutput is false")
	}
}

// --- nil error safety ---

func TestHumanRenderer_RenderError_Nil(t *testing.T) {
	var buf bytes.Buffer
	r := NewHumanRenderer(&buf)

	r.RenderError(nil) // must not panic

	testutil.AssertEqual(t, buf.String(), "")
}

func TestJSONRenderer_RenderError_Nil(t *testing.T) {
	var buf bytes.Buffer
	r := NewJSONRenderer(&buf)

	r.RenderError(nil) // must not panic

	testutil.AssertEqual(t, buf.String(), "")
}

// --- silentErr ---

func TestSilentErr_SkipsMainRender(t *testing.T) {
	inner := errors.New("doctor checks failed")
	se := &silentErr{inner}

	// Verify it implements error and Unwrap.
	testutil.AssertEqual(t, se.Error(), "doctor checks failed")

	var target *silentErr
	if !errors.As(se, &target) {
		t.Error("expected errors.As to match silentErr")
	}
}

// --- JSON error-path tests ---

func TestJSONRenderer_DoctorConfigError_SingleOutput(t *testing.T) {
	// Simulate: config load fails, main renders the error (not doctor).
	configErr := fmt.Errorf("load config: %w", errors.New("file not found"))

	var buf bytes.Buffer
	r := NewJSONRenderer(&buf)
	r.RenderError(configErr)

	var env Envelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	testutil.AssertEqual(t, env.Version, 1)
	if env.Error == nil {
		t.Fatal("expected error in envelope")
	}
	testutil.AssertEqual(t, env.Error.Code, "INTERNAL_ERROR")
	if !strings.Contains(env.Error.Message, "load config") {
		t.Errorf("expected config error message, got: %s", env.Error.Message)
	}
}

func TestJSONRenderer_DoctorFailure_SilentErr(t *testing.T) {
	// When doctor checks fail, errDoctorFailed is wrapped in silentErr.
	// Main should skip rendering. Verify silentErr wraps correctly.
	inner := errors.New("doctor checks failed")
	se := &silentErr{inner}

	// Verify the error message passes through.
	testutil.AssertEqual(t, se.Error(), "doctor checks failed")

	// Verify errors.As matches.
	var target *silentErr
	if !errors.As(se, &target) {
		t.Fatal("silentErr should be matchable via errors.As")
	}
}
