// Package dryrun provides in-process fake implementations of
// domain.ImageGenerator and domain.TTSSynthesizer that produce valid
// placeholder PNG and WAV files at the requested OutputPath without making
// any external API calls. It exists so the operator can iterate on Phase B
// prompts (image / TTS) locally without burning DashScope credits.
//
// The fakes mirror the real DashScope clients at the interface boundary —
// the Phase B image and TTS tracks remain untouched and simply receive a
// different ImageGenerator / TTSSynthesizer at construction time when
// PipelineConfig.DryRun is true.
package dryrun
