package eval

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// ShadowCase is a single recent passed Critic case selected for Shadow replay.
// CreatedAt is the raw SQLite timestamp string; Shadow never compares it as
// time.Time because the DB text is the canonical ordering key.
type ShadowCase struct {
	RunID           string
	CreatedAt       string
	ScenarioPath    string
	BaselineScore   float64
	BaselineVerdict string
}

// ShadowSource produces the most recent passed Critic cases to be replayed.
// Shadow unit tests inject an inline fake; production wires
// SQLiteShadowSource over the live runs DB.
type ShadowSource interface {
	RecentPassedCases(ctx context.Context, limit int) ([]ShadowCase, error)
}

// CriticPassThreshold is the operational score cutoff already used by the
// metrics/defect-escape path. Story 4.2 reuses it as the Shadow selection
// predicate rather than introducing a second pass-definition knob.
const CriticPassThreshold = 0.70

// SQLiteShadowSource implements ShadowSource over the runs table using a
// narrow *sql.DB handle. Keeping the adapter here — instead of in
// internal/db — preserves the existing layer-import rules (eval already
// owns the interface; adding an internal/db → internal/critic/eval edge
// would create an import cycle through testutil).
type SQLiteShadowSource struct {
	db *sql.DB
}

// NewSQLiteShadowSource constructs a SQLiteShadowSource backed by db. The
// constructor performs no DB I/O; callers keep ownership of the handle.
func NewSQLiteShadowSource(db *sql.DB) *SQLiteShadowSource {
	return &SQLiteShadowSource{db: db}
}

// recentPassedCasesSQL mirrors the Story 2.7 completed-run recency pattern
// (WHERE status='completed' ORDER BY created_at DESC LIMIT ?) so the planner
// reuses idx_runs_status_created_at. The additional scenario_path /
// critic_score predicates are index-friendly: they filter inside the
// index-driven iteration rather than forcing a full table scan.
const recentPassedCasesSQL = `
SELECT id, created_at, scenario_path, critic_score
  FROM runs
 WHERE status = 'completed'
   AND scenario_path IS NOT NULL
   AND critic_score IS NOT NULL
   AND critic_score >= ?
 ORDER BY created_at DESC, id DESC
 LIMIT ?`

// RecentPassedCases returns up to limit most recent completed runs whose
// critic_score meets the operational pass threshold. Ordering is
// deterministic: created_at DESC, id DESC. BaselineVerdict is always "pass"
// in V1 because the run-level critic_score cutoff is the only persisted
// pass signal available today.
func (s *SQLiteShadowSource) RecentPassedCases(ctx context.Context, limit int) ([]ShadowCase, error) {
	if limit < 1 {
		return nil, fmt.Errorf("shadow recent passed cases: limit %d must be > 0: %w", limit, domain.ErrValidation)
	}

	rows, err := s.db.QueryContext(ctx, recentPassedCasesSQL, CriticPassThreshold, limit)
	if err != nil {
		return nil, fmt.Errorf("shadow recent passed cases query: %w", err)
	}
	defer rows.Close()

	var out []ShadowCase
	for rows.Next() {
		var (
			c            ShadowCase
			scenarioPath sql.NullString
			score        sql.NullFloat64
		)
		if err := rows.Scan(&c.RunID, &c.CreatedAt, &scenarioPath, &score); err != nil {
			return nil, fmt.Errorf("shadow recent passed cases scan: %w", err)
		}
		c.ScenarioPath = scenarioPath.String
		c.BaselineScore = score.Float64
		c.BaselineVerdict = "pass"
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("shadow recent passed cases iterate: %w", err)
	}
	return out, nil
}
