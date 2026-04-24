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
	Checks   []CheckResult `json:"checks"`
	Passed   bool          `json:"passed"`
	Warnings []string      `json:"warnings,omitempty"`
}

// GoldenAddOutput is the structured output for `pipeline golden add`.
type GoldenAddOutput struct {
	Index        int    `json:"index"`
	CreatedAt    string `json:"created_at"`
	PositivePath string `json:"positive_path"`
	NegativePath string `json:"negative_path"`
	PairCount    int    `json:"pair_count"`
}

// GoldenListOutput is the structured output for `pipeline golden list`.
type GoldenListOutput struct {
	Pairs []GoldenPairRow `json:"pairs"`
}

// GoldenPairRow is one row in the `pipeline golden list` output.
type GoldenPairRow struct {
	Index        int    `json:"index"`
	CreatedAt    string `json:"created_at"`
	PositivePath string `json:"positive_path"`
	NegativePath string `json:"negative_path"`
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

	// Story 2.6 — HITL pause fields. Emitted only when the run is in a
	// HITL wait state (status=waiting AND stage ∈ HITL stages).
	PausedPosition   *PausedPositionOutput  `json:"paused_position,omitempty"`
	DecisionsSummary *DecisionSummaryOutput `json:"decisions_summary,omitempty"`
	Summary          string                 `json:"summary,omitempty"`
	ChangesSince     []ChangeOutput         `json:"changes_since_last_interaction,omitempty"`
}

// PausedPositionOutput mirrors domain.HITLSession in the CLI JSON envelope.
type PausedPositionOutput struct {
	Stage                    string `json:"stage"`
	SceneIndex               int    `json:"scene_index"`
	LastInteractionTimestamp string `json:"last_interaction_timestamp"`
}

// DecisionSummaryOutput is the triplet surfaced next to a paused run.
type DecisionSummaryOutput struct {
	ApprovedCount int `json:"approved_count"`
	RejectedCount int `json:"rejected_count"`
	PendingCount  int `json:"pending_count"`
}

// ChangeOutput is one item in the FR50 "what changed since pause" diff.
type ChangeOutput struct {
	Kind      string `json:"kind"`
	SceneID   string `json:"scene_id"`
	Before    string `json:"before,omitempty"`
	After     string `json:"after,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
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

// CleanOutput is the Story 10.3 structured output for `pipeline clean`.
// It mirrors service.CleanSummary verbatim so human and JSON renderers can
// consume the same payload without an extra transform.
type CleanOutput struct {
	RetentionDays int                `json:"retention_days"`
	CutoffUTC     string             `json:"cutoff_utc"`
	RunsScanned   int                `json:"runs_scanned"`
	RunsArchived  int                `json:"runs_archived"`
	FilesDeleted  int                `json:"files_deleted"`
	DBRefsCleared int                `json:"db_refs_cleared"`
	Vacuum        string             `json:"vacuum"`
	VacuumError   string             `json:"vacuum_error,omitempty"`
	ArchivedRuns  []CleanArchivedRun `json:"archived_runs,omitempty"`
}

// CleanArchivedRun is one entry in CleanOutput.ArchivedRuns.
type CleanArchivedRun struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	UpdatedAt     string `json:"updated_at"`
	FilesDeleted  int    `json:"files_deleted"`
	DBRefsCleared int    `json:"db_refs_cleared"`
}

type ExportOutput struct {
	RunID   string `json:"run_id"`
	Type    string `json:"type"`
	Format  string `json:"format"`
	Path    string `json:"path"`
	Records int    `json:"records"`
}

// ResumeOutput is the structured output for the resume command.
// Warnings carries filesystem/DB inconsistency descriptions that were
// bypassed via --force (empty when no mismatches were present).
// Summary + ChangesSince are populated (when relevant) from a post-resume
// HITLService.BuildStatus call so the operator sees the state-aware
// summary and the FR50 "what changed since you paused" diff at the moment
// of re-engagement.
type ResumeOutput struct {
	Run          RunOutput      `json:"run"`
	Warnings     []string       `json:"warnings,omitempty"`
	Summary      string         `json:"summary,omitempty"`
	ChangesSince []ChangeOutput `json:"changes_since_last_interaction,omitempty"`
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
	case *domain.MetricsReport:
		r.renderMetrics(v)
	case *GoldenAddOutput:
		r.renderGoldenAdd(v)
	case *GoldenListOutput:
		r.renderGoldenList(v)
	case *CleanOutput:
		r.renderClean(v)
	case *ExportOutput:
		r.renderExport(v)
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
	if len(d.Warnings) > 0 {
		fmt.Fprintln(r.w)
		for _, w := range d.Warnings {
			fmt.Fprintf(r.w, "%s\u26a0 %s%s\n", colorYellow, w, colorReset)
		}
	}
}

func (r *HumanRenderer) renderGoldenAdd(o *GoldenAddOutput) {
	fmt.Fprintf(r.w, "%sGolden pair added:%s\n", colorGreen, colorReset)
	fmt.Fprintf(r.w, "  Index:    %d\n", o.Index)
	fmt.Fprintf(r.w, "  Created:  %s\n", o.CreatedAt)
	fmt.Fprintf(r.w, "  Positive: %s\n", o.PositivePath)
	fmt.Fprintf(r.w, "  Negative: %s\n", o.NegativePath)
	fmt.Fprintf(r.w, "  Total pairs: %d\n", o.PairCount)
}

func (r *HumanRenderer) renderGoldenList(o *GoldenListOutput) {
	if len(o.Pairs) == 0 {
		fmt.Fprintln(r.w, "No Golden pairs registered.")
		return
	}
	for _, p := range o.Pairs {
		fmt.Fprintf(r.w, "  [%d] %s  pos=%s  neg=%s\n",
			p.Index, p.CreatedAt, p.PositivePath, p.NegativePath)
	}
	fmt.Fprintf(r.w, "\n%d pair(s) total\n", len(o.Pairs))
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
	r.renderHITLBlock(o.Summary, o.PausedPosition, o.ChangesSince)
}

// renderHITLBlock emits the Story 2.6 summary line and the FR50 change diff
// block when populated. No-ops when summary is empty (non-HITL runs).
func (r *HumanRenderer) renderHITLBlock(summary string, paused *PausedPositionOutput, changes []ChangeOutput) {
	if summary == "" {
		return
	}
	fmt.Fprintln(r.w)
	fmt.Fprintf(r.w, "  Summary: %s\n", summary)
	if len(changes) > 0 {
		anchor := ""
		if paused != nil {
			anchor = " (" + paused.LastInteractionTimestamp + ")"
		}
		fmt.Fprintf(r.w, "\n  %sChanges since last interaction%s%s:\n", colorYellow, anchor, colorReset)
		for _, c := range changes {
			ts := ""
			if c.Timestamp != "" {
				ts = " (at " + c.Timestamp + ")"
			}
			switch c.Kind {
			case "scene_status_flipped":
				fmt.Fprintf(r.w, "    \u2022 scene %s: %s \u2192 %s%s\n", c.SceneID, c.Before, c.After, ts)
			case "scene_added":
				fmt.Fprintf(r.w, "    \u2022 scene %s added (%s)%s\n", c.SceneID, c.After, ts)
			case "scene_removed":
				fmt.Fprintf(r.w, "    \u2022 scene %s removed (was %s)%s\n", c.SceneID, c.Before, ts)
			default:
				fmt.Fprintf(r.w, "    \u2022 scene %s: %s\n", c.SceneID, c.Kind)
			}
		}
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
	r.renderHITLBlock(o.Summary, o.Run.PausedPosition, o.ChangesSince)
}

func (r *HumanRenderer) renderClean(o *CleanOutput) {
	fmt.Fprintf(r.w, "%sSoft archive complete%s\n", colorGreen, colorReset)
	fmt.Fprintf(r.w, "  Retention:     %d days\n", o.RetentionDays)
	fmt.Fprintf(r.w, "  Cutoff (UTC):  %s\n", o.CutoffUTC)
	fmt.Fprintf(r.w, "  Runs scanned:  %d\n", o.RunsScanned)
	fmt.Fprintf(r.w, "  Runs archived: %d\n", o.RunsArchived)
	fmt.Fprintf(r.w, "  Files deleted: %d\n", o.FilesDeleted)
	fmt.Fprintf(r.w, "  DB refs cleared: %d\n", o.DBRefsCleared)
	switch o.Vacuum {
	case "ran":
		fmt.Fprintf(r.w, "  VACUUM:        %sran%s\n", colorGreen, colorReset)
	case "skipped_active_runs":
		fmt.Fprintf(r.w, "  VACUUM:        %sskipped (active runs present)%s\n", colorYellow, colorReset)
	case "failed":
		fmt.Fprintf(r.w, "  VACUUM:        %sfailed%s — %s\n", colorRed, colorReset, o.VacuumError)
	default:
		fmt.Fprintf(r.w, "  VACUUM:        %s\n", o.Vacuum)
	}
}

func (r *HumanRenderer) renderExport(o *ExportOutput) {
	fmt.Fprintf(r.w, "%sExport complete%s\n", colorGreen, colorReset)
	fmt.Fprintf(r.w, "  Run ID:     %s\n", o.RunID)
	fmt.Fprintf(r.w, "  Type:       %s\n", o.Type)
	fmt.Fprintf(r.w, "  Format:     %s\n", o.Format)
	fmt.Fprintf(r.w, "  Records:    %d\n", o.Records)
	fmt.Fprintf(r.w, "  File:       %s\n", o.Path)
}

func (r *HumanRenderer) renderMetrics(m *domain.MetricsReport) {
	fmt.Fprintf(r.w, "Pipeline metrics — rolling window: %d (%d completed runs)\n", m.Window, m.WindowCount)
	if m.Provisional {
		fmt.Fprintf(r.w, "%s[provisional — n < %d]%s\n", colorYellow, m.Window, colorReset)
	}
	fmt.Fprintln(r.w)
	fmt.Fprintf(r.w, "%-27s  %-11s  %-9s  %-10s\n", "METRIC", "VALUE", "TARGET", "STATUS")
	fmt.Fprintf(r.w, "%-27s  %-11s  %-9s  %-10s\n",
		"---------------------------", "-----------", "---------", "----------")
	for _, metric := range m.Metrics {
		value := "—"
		if metric.Value != nil {
			if metric.ID == domain.MetricCriticCalibration {
				value = fmt.Sprintf("%.2f", *metric.Value)
			} else {
				value = fmt.Sprintf("%.1f%%", *metric.Value*100)
			}
		}

		target := fmt.Sprintf("≥ %.0f%%", metric.Target*100)
		if metric.ID == domain.MetricCriticCalibration {
			target = fmt.Sprintf("≥ %.2f", metric.Target)
		} else if metric.Comparator == domain.ComparatorLTE {
			target = fmt.Sprintf("≤ %.0f%%", metric.Target*100)
		} else if metric.Target == 1.0 {
			// "≥ 100%" is semantically odd; spec example shows bare "100%".
			target = "100%"
		}

		statusText := "unavailable"
		statusColor := colorYellow
		if !metric.Unavailable && metric.Pass {
			statusText = "✓ pass"
			statusColor = colorGreen
		}
		if !metric.Unavailable && !metric.Pass {
			statusText = "✗ fail"
			statusColor = colorRed
		}

		fmt.Fprintf(r.w, "%-27s  %-11s  %-9s  %s%-10s%s\n",
			metric.Label, value, target, statusColor, statusText, colorReset)
	}
	fmt.Fprintln(r.w)
	fmt.Fprintf(r.w, "Generated at: %s\n", m.GeneratedAt)
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
