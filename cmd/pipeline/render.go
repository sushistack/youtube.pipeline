package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// ANSI color constants — no external color library.
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
)

// Renderer is the output abstraction for CLI commands.
type Renderer interface {
	RenderSuccess(data any)
	RenderError(err error)
}

// --- JSON envelope types ---

// Envelope is the versioned JSON wrapper for all CLI output.
type Envelope struct {
	Version int        `json:"version"`
	Data    any        `json:"data,omitempty"`
	Error   *ErrorInfo `json:"error,omitempty"`
}

// ErrorInfo carries classified error details in JSON output.
type ErrorInfo struct {
	Code        string `json:"code"`
	Message     string `json:"message"`
	Recoverable bool   `json:"recoverable"`
}

// --- Output data types ---

// DoctorOutput is the structured output for the doctor command.
type DoctorOutput struct {
	Checks []CheckResult `json:"checks"`
	Passed bool          `json:"passed"`
}

// CheckResult is one item in a doctor report.
type CheckResult struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Message string `json:"message,omitempty"`
}

// InitOutput is the structured output for the init command.
type InitOutput struct {
	Config   string `json:"config"`
	Env      string `json:"env"`
	Database string `json:"database"`
	Output   string `json:"output"`
}

// silentErr wraps an error that has already been rendered by the command.
// main's error handler skips JSON rendering for these.
type silentErr struct{ err error }

func (e *silentErr) Error() string { return e.err.Error() }
func (e *silentErr) Unwrap() error { return e.err }

// RunOutput is the structured output for create/get/status single-run commands.
type RunOutput struct {
	ID        string `json:"id"`
	SCPID     string `json:"scp_id"`
	Stage     string `json:"stage"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at,omitempty"`
	OutputDir string `json:"output_dir,omitempty"`
}

// RunListOutput is the structured output for `pipeline status` (all runs).
type RunListOutput struct {
	Runs  []RunOutput `json:"runs"`
	Total int         `json:"total"`
}

// CancelOutput is the structured output for the cancel command.
type CancelOutput struct {
	RunID  string `json:"run_id"`
	Status string `json:"status"`
}

// ResumeOutput is the structured output for the resume command.
// Warnings carries filesystem/DB inconsistency descriptions that were
// bypassed via --force (empty when no mismatches were present).
type ResumeOutput struct {
	Run      RunOutput `json:"run"`
	Warnings []string  `json:"warnings,omitempty"`
}

// --- HumanRenderer ---

// HumanRenderer writes color-coded output to an io.Writer.
type HumanRenderer struct {
	w io.Writer
}

// NewHumanRenderer creates a HumanRenderer that writes to w.
func NewHumanRenderer(w io.Writer) *HumanRenderer {
	return &HumanRenderer{w: w}
}

// RenderSuccess writes human-readable output, type-switching on known types.
func (r *HumanRenderer) RenderSuccess(data any) {
	switch v := data.(type) {
	case *DoctorOutput:
		r.renderDoctor(v)
	case *InitOutput:
		r.renderInit(v)
	case *RunOutput:
		r.renderRun(v)
	case *RunListOutput:
		r.renderRunList(v)
	case *CancelOutput:
		r.renderCancel(v)
	case *ResumeOutput:
		r.renderResume(v)
	default:
		fmt.Fprintf(r.w, "%v\n", data)
	}
}

// RenderError writes a red error message. Nil errors are ignored.
func (r *HumanRenderer) RenderError(err error) {
	if err == nil {
		return
	}
	fmt.Fprintf(r.w, "%sError: %s%s\n", colorRed, err.Error(), colorReset)
}

func (r *HumanRenderer) renderDoctor(d *DoctorOutput) {
	fmt.Fprintln(r.w, "Doctor checks:")
	passed := 0
	for _, c := range d.Checks {
		if c.Passed {
			fmt.Fprintf(r.w, "  %s\u2713 %s%s\n", colorGreen, c.Name, colorReset)
			passed++
		} else {
			fmt.Fprintf(r.w, "  %s\u2717 %s%s: %s\n", colorRed, c.Name, colorReset, c.Message)
		}
	}
	fmt.Fprintln(r.w)
	fmt.Fprintf(r.w, "%d/%d checks passed", passed, len(d.Checks))
	if !d.Passed {
		fmt.Fprint(r.w, " \u2014 fix failing checks before running the pipeline")
	}
	fmt.Fprintln(r.w)
}

func (r *HumanRenderer) renderInit(o *InitOutput) {
	fmt.Fprintf(r.w, "%sInitialized youtube.pipeline:%s\n", colorGreen, colorReset)
	fmt.Fprintf(r.w, "  Config:   %s\n", o.Config)
	fmt.Fprintf(r.w, "  Env:      %s\n", o.Env)
	fmt.Fprintf(r.w, "  Database: %s\n", o.Database)
	fmt.Fprintf(r.w, "  Output:   %s\n", o.Output)
}

func (r *HumanRenderer) renderRun(o *RunOutput) {
	fmt.Fprintf(r.w, "%sRun:%s %s\n", colorGreen, colorReset, o.ID)
	fmt.Fprintf(r.w, "  SCP ID:    %s\n", o.SCPID)
	fmt.Fprintf(r.w, "  Stage:     %s\n", o.Stage)
	fmt.Fprintf(r.w, "  Status:    %s\n", o.Status)
	fmt.Fprintf(r.w, "  Created:   %s\n", o.CreatedAt)
	if o.UpdatedAt != "" {
		fmt.Fprintf(r.w, "  Updated:   %s\n", o.UpdatedAt)
	}
	if o.OutputDir != "" {
		fmt.Fprintf(r.w, "  Output:    %s\n", o.OutputDir)
	}
}

func (r *HumanRenderer) renderRunList(o *RunListOutput) {
	if o.Total == 0 {
		fmt.Fprintf(r.w, "No runs found.\n")
		return
	}
	fmt.Fprintf(r.w, "%-30s  %-16s  %-12s  %s\n", "RUN ID", "STAGE", "STATUS", "CREATED")
	fmt.Fprintf(r.w, "%-30s  %-16s  %-12s  %s\n",
		"------------------------------", "----------------", "------------", "-------------------")
	for _, run := range o.Runs {
		created := run.CreatedAt
		if len(created) > 19 {
			created = created[:19]
		}
		fmt.Fprintf(r.w, "%-30s  %-16s  %-12s  %s\n", run.ID, run.Stage, run.Status, created)
	}
	fmt.Fprintf(r.w, "\n%d run(s) total\n", o.Total)
}

func (r *HumanRenderer) renderCancel(o *CancelOutput) {
	fmt.Fprintf(r.w, "%sCancelled:%s %s\n", colorYellow, colorReset, o.RunID)
}

func (r *HumanRenderer) renderResume(o *ResumeOutput) {
	fmt.Fprintf(r.w, "%sResumed:%s %s\n", colorGreen, colorReset, o.Run.ID)
	fmt.Fprintf(r.w, "  Stage:     %s\n", o.Run.Stage)
	fmt.Fprintf(r.w, "  Status:    %s\n", o.Run.Status)
	if o.Run.UpdatedAt != "" {
		fmt.Fprintf(r.w, "  Updated:   %s\n", o.Run.UpdatedAt)
	}
	if len(o.Warnings) > 0 {
		fmt.Fprintf(r.w, "%sWarnings:%s\n", colorYellow, colorReset)
		for _, w := range o.Warnings {
			fmt.Fprintf(r.w, "  - %s\n", w)
		}
	}
}

// --- JSONRenderer ---

// JSONRenderer writes versioned JSON envelopes to an io.Writer.
type JSONRenderer struct {
	w io.Writer
}

// NewJSONRenderer creates a JSONRenderer that writes to w.
func NewJSONRenderer(w io.Writer) *JSONRenderer {
	return &JSONRenderer{w: w}
}

// RenderSuccess writes a versioned JSON success envelope.
func (r *JSONRenderer) RenderSuccess(data any) {
	env := Envelope{
		Version: 1,
		Data:    data,
	}
	json.NewEncoder(r.w).Encode(env)
}

// RenderError writes a versioned JSON error envelope using domain.Classify.
// Nil errors are ignored.
func (r *JSONRenderer) RenderError(err error) {
	if err == nil {
		return
	}
	_, code, recoverable := domain.Classify(err)
	env := Envelope{
		Version: 1,
		Error: &ErrorInfo{
			Code:        code,
			Message:     err.Error(),
			Recoverable: recoverable,
		},
	}
	json.NewEncoder(r.w).Encode(env)
}
