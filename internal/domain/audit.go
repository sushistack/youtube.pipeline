package domain

import (
	"context"
	"time"
)

// AuditEventType categorises the kind of provider interaction being logged.
type AuditEventType string

const (
	AuditEventTextGeneration  AuditEventType = "text_generation"
	AuditEventImageGeneration AuditEventType = "image_generation"
	AuditEventTTSSynthesis    AuditEventType = "tts_synthesis"
	AuditEventVoiceBlocked    AuditEventType = "voice_blocked"
	AuditEventPolisherFailed  AuditEventType = "polisher_failed"
)

// AuditEntry is a single NDJSON line written to audit.log.
type AuditEntry struct {
	Timestamp time.Time      `json:"timestamp"`
	EventType AuditEventType `json:"event_type"`
	RunID     string         `json:"run_id"`
	Stage     string         `json:"stage"`
	Provider  string         `json:"provider"`
	Model     string         `json:"model"`
	Prompt    string         `json:"prompt"`              // truncated to 2048 chars
	CostUSD   float64        `json:"cost_usd"`
	BlockedID string         `json:"blocked_id,omitempty"` // populated only for voice_blocked events
}

// AuditLogger is the port for recording audit entries. The interface lives
// in domain so that agents (which import domain) can depend on it without
// causing circular imports with internal/pipeline.
type AuditLogger interface {
	Log(ctx context.Context, entry AuditEntry) error
}
