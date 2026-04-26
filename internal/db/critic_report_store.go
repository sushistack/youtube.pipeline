package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// CriticReportStore persists Phase A critic checkpoint reports and the
// narration attempts they evaluated. Every attempt (pass/retry/accept) is
// retained so retry verdicts can be audited and writer prompts iterated
// against concrete failures. attempt_number is the run's retry_count at
// Phase A completion + 1 (1-indexed).
type CriticReportStore struct {
	db *sql.DB
}

func NewCriticReportStore(db *sql.DB) *CriticReportStore {
	return &CriticReportStore{db: db}
}

// InsertCriticReport persists a single critic checkpoint report. checkpoint
// must be CriticCheckpointPostWriter or CriticCheckpointPostReviewer.
func (s *CriticReportStore) InsertCriticReport(
	ctx context.Context,
	runID string,
	attemptNumber int,
	report domain.CriticCheckpointReport,
) error {
	if runID == "" {
		return fmt.Errorf("insert critic report: %w: run_id is empty", domain.ErrValidation)
	}
	if attemptNumber < 1 {
		return fmt.Errorf("insert critic report %s: %w: attempt_number %d must be >= 1", runID, domain.ErrValidation, attemptNumber)
	}

	rubricJSON, err := json.Marshal(report.Rubric)
	if err != nil {
		return fmt.Errorf("insert critic report %s: marshal rubric: %w", runID, err)
	}
	sceneNotes := report.SceneNotes
	if sceneNotes == nil {
		sceneNotes = []domain.CriticSceneNote{}
	}
	sceneNotesJSON, err := json.Marshal(sceneNotes)
	if err != nil {
		return fmt.Errorf("insert critic report %s: marshal scene_notes: %w", runID, err)
	}
	precheckJSON, err := json.Marshal(report.Precheck)
	if err != nil {
		return fmt.Errorf("insert critic report %s: marshal precheck: %w", runID, err)
	}

	_, err = s.db.ExecContext(ctx, `
INSERT INTO critic_reports (
    run_id, checkpoint, attempt_number, verdict, retry_reason,
    overall_score, rubric_json, feedback, scene_notes_json, precheck_json,
    critic_model, critic_provider, source_version
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		runID,
		report.Checkpoint,
		attemptNumber,
		report.Verdict,
		nullIfEmpty(report.RetryReason),
		report.OverallScore,
		string(rubricJSON),
		report.Feedback,
		string(sceneNotesJSON),
		string(precheckJSON),
		nullIfEmpty(report.CriticModel),
		nullIfEmpty(report.CriticProvider),
		nullIfEmpty(report.SourceVersion),
	)
	if err != nil {
		return fmt.Errorf("insert critic report %s: %w", runID, err)
	}
	return nil
}

// InsertNarrationAttempt persists the full narration script the critic
// evaluated. The script is stored as JSON to preserve every field
// (act_id, fact_tags, atmosphere, etc.) for downstream diagnosis.
func (s *CriticReportStore) InsertNarrationAttempt(
	ctx context.Context,
	runID string,
	attemptNumber int,
	narration *domain.NarrationScript,
) error {
	if runID == "" {
		return fmt.Errorf("insert narration attempt: %w: run_id is empty", domain.ErrValidation)
	}
	if attemptNumber < 1 {
		return fmt.Errorf("insert narration attempt %s: %w: attempt_number %d must be >= 1", runID, domain.ErrValidation, attemptNumber)
	}
	if narration == nil {
		return fmt.Errorf("insert narration attempt %s: %w: narration is nil", runID, domain.ErrValidation)
	}

	raw, err := json.Marshal(narration)
	if err != nil {
		return fmt.Errorf("insert narration attempt %s: marshal narration: %w", runID, err)
	}

	_, err = s.db.ExecContext(ctx, `
INSERT INTO narration_attempts (run_id, attempt_number, narration_json)
VALUES (?, ?, ?)`,
		runID, attemptNumber, string(raw),
	)
	if err != nil {
		return fmt.Errorf("insert narration attempt %s: %w", runID, err)
	}
	return nil
}

// ListCriticReportsByRun returns every persisted critic report for a run,
// ordered by created_at ascending. Returns (nil, nil) when the run has
// no reports.
func (s *CriticReportStore) ListCriticReportsByRun(
	ctx context.Context,
	runID string,
) ([]domain.PersistedCriticReport, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT run_id, checkpoint, attempt_number, verdict, retry_reason,
       overall_score, rubric_json, feedback, scene_notes_json, precheck_json,
       critic_model, critic_provider, source_version, created_at
  FROM critic_reports
 WHERE run_id = ?
 ORDER BY created_at ASC, id ASC`, runID)
	if err != nil {
		return nil, fmt.Errorf("list critic reports for %s: %w", runID, err)
	}
	defer rows.Close()

	var out []domain.PersistedCriticReport
	for rows.Next() {
		var (
			rec            domain.PersistedCriticReport
			retryReason    sql.NullString
			rubricJSON     string
			sceneNotesJSON string
			precheckJSON   string
			criticModel    sql.NullString
			criticProvider sql.NullString
			sourceVersion  sql.NullString
		)
		if err := rows.Scan(
			&rec.RunID, &rec.Report.Checkpoint, &rec.AttemptNumber,
			&rec.Report.Verdict, &retryReason,
			&rec.Report.OverallScore, &rubricJSON, &rec.Report.Feedback,
			&sceneNotesJSON, &precheckJSON,
			&criticModel, &criticProvider, &sourceVersion, &rec.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("list critic reports for %s scan: %w", runID, err)
		}
		if retryReason.Valid {
			rec.Report.RetryReason = retryReason.String
		}
		if err := json.Unmarshal([]byte(rubricJSON), &rec.Report.Rubric); err != nil {
			return nil, fmt.Errorf("list critic reports for %s decode rubric: %w", runID, err)
		}
		if err := json.Unmarshal([]byte(sceneNotesJSON), &rec.Report.SceneNotes); err != nil {
			return nil, fmt.Errorf("list critic reports for %s decode scene_notes: %w", runID, err)
		}
		if err := json.Unmarshal([]byte(precheckJSON), &rec.Report.Precheck); err != nil {
			return nil, fmt.Errorf("list critic reports for %s decode precheck: %w", runID, err)
		}
		if criticModel.Valid {
			rec.Report.CriticModel = criticModel.String
		}
		if criticProvider.Valid {
			rec.Report.CriticProvider = criticProvider.String
		}
		if sourceVersion.Valid {
			rec.Report.SourceVersion = sourceVersion.String
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list critic reports for %s iterate: %w", runID, err)
	}
	return out, nil
}

// GetLastCriticReport returns the full report from the most recent critic
// entry for the run. Returns (nil, nil) when no reports exist. Used by the
// engine to inject prior rubric scores + feedback into the next writer attempt.
func (s *CriticReportStore) GetLastCriticReport(ctx context.Context, runID string) (*domain.CriticCheckpointReport, error) {
	var (
		report         domain.CriticCheckpointReport
		retryReason    sql.NullString
		rubricJSON     string
		sceneNotesJSON string
		precheckJSON   string
	)
	err := s.db.QueryRowContext(ctx, `
SELECT verdict, retry_reason, overall_score, rubric_json, feedback, scene_notes_json, precheck_json
  FROM critic_reports
 WHERE run_id = ?
 ORDER BY created_at DESC, id DESC
 LIMIT 1`, runID).Scan(
		&report.Verdict, &retryReason, &report.OverallScore,
		&rubricJSON, &report.Feedback, &sceneNotesJSON, &precheckJSON,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get last critic report %s: %w", runID, err)
	}
	if retryReason.Valid {
		report.RetryReason = retryReason.String
	}
	if err := json.Unmarshal([]byte(rubricJSON), &report.Rubric); err != nil {
		return nil, fmt.Errorf("get last critic report %s decode rubric: %w", runID, err)
	}
	return &report, nil
}

// ListNarrationAttemptsByRun returns every persisted narration attempt for a
// run, ordered by created_at ascending. Returns (nil, nil) when none exist.
func (s *CriticReportStore) ListNarrationAttemptsByRun(
	ctx context.Context,
	runID string,
) ([]domain.PersistedNarrationAttempt, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT run_id, attempt_number, narration_json, created_at
  FROM narration_attempts
 WHERE run_id = ?
 ORDER BY created_at ASC, id ASC`, runID)
	if err != nil {
		return nil, fmt.Errorf("list narration attempts for %s: %w", runID, err)
	}
	defer rows.Close()

	var out []domain.PersistedNarrationAttempt
	for rows.Next() {
		var (
			rec           domain.PersistedNarrationAttempt
			narrationJSON string
		)
		if err := rows.Scan(&rec.RunID, &rec.AttemptNumber, &narrationJSON, &rec.CreatedAt); err != nil {
			return nil, fmt.Errorf("list narration attempts for %s scan: %w", runID, err)
		}
		var narration domain.NarrationScript
		if err := json.Unmarshal([]byte(narrationJSON), &narration); err != nil {
			return nil, fmt.Errorf("list narration attempts for %s decode narration: %w", runID, err)
		}
		rec.Narration = &narration
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list narration attempts for %s iterate: %w", runID, err)
	}
	return out, nil
}
