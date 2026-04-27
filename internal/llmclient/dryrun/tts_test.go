package dryrun_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/llmclient/dryrun"
)

func TestTTSClient_ImplementsTTSSynthesizer(t *testing.T) {
	t.Parallel()
	var _ domain.TTSSynthesizer = dryrun.NewTTSClient()
}

func TestTTSClient_Synthesize_WritesValidWAV(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out := filepath.Join(dir, "tts", "scene_01.wav")

	client := dryrun.NewTTSClient()
	resp, err := client.Synthesize(context.Background(), domain.TTSRequest{
		Text:       "안녕하세요. 오늘은 SCP-049 이야기를 들려드립니다.",
		Model:      "qwen3-tts-flash-2025-09-18",
		Voice:      "Ethan",
		OutputPath: out,
		Format:     "wav",
	})
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}

	if resp.AudioPath != out {
		t.Errorf("AudioPath = %q, want %q", resp.AudioPath, out)
	}
	if resp.Provider != dryrun.Provider {
		t.Errorf("Provider = %q, want %q", resp.Provider, dryrun.Provider)
	}
	if resp.CostUSD != 0 {
		t.Errorf("CostUSD = %v, want 0", resp.CostUSD)
	}
	if resp.Model != "qwen3-tts-flash-2025-09-18" {
		t.Errorf("Model = %q, want echo of request model", resp.Model)
	}
	if resp.DurationMs <= 0 {
		t.Errorf("DurationMs = %d, want > 0", resp.DurationMs)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read wav: %v", err)
	}

	// RIFF/WAVE header sanity.
	if len(data) < 44 {
		t.Fatalf("wav too short: %d bytes", len(data))
	}
	if !bytes.Equal(data[0:4], []byte("RIFF")) {
		t.Errorf("missing RIFF magic")
	}
	if !bytes.Equal(data[8:12], []byte("WAVE")) {
		t.Errorf("missing WAVE marker")
	}
	if !bytes.Equal(data[12:16], []byte("fmt ")) {
		t.Errorf("missing fmt chunk")
	}
	if !bytes.Equal(data[36:40], []byte("data")) {
		t.Errorf("missing data chunk")
	}

	// Format params must match production constants exactly so concat with
	// `-c copy` works across chunks and the production duration estimator
	// returns the same value our client reports.
	audioFormat := binary.LittleEndian.Uint16(data[20:22])
	numChannels := binary.LittleEndian.Uint16(data[22:24])
	sampleRate := binary.LittleEndian.Uint32(data[24:28])
	byteRate := binary.LittleEndian.Uint32(data[28:32])
	bitsPerSample := binary.LittleEndian.Uint16(data[34:36])

	if audioFormat != 1 {
		t.Errorf("AudioFormat = %d, want 1 (PCM)", audioFormat)
	}
	if numChannels != 1 {
		t.Errorf("NumChannels = %d, want 1 (mono)", numChannels)
	}
	if sampleRate != 44100 {
		t.Errorf("SampleRate = %d, want 44100", sampleRate)
	}
	if bitsPerSample != 16 {
		t.Errorf("BitsPerSample = %d, want 16", bitsPerSample)
	}
	if byteRate != 88_200 {
		t.Errorf("ByteRate = %d, want 88200", byteRate)
	}

	// Production duration estimator (dashscope/tts.go:188-195) divides total
	// file bytes by 176_400. Our DurationMs MUST equal that formula.
	wantDuration := int64(len(data)) * 1000 / 176_400
	if resp.DurationMs != wantDuration {
		t.Errorf("DurationMs = %d, want %d (production formula)", resp.DurationMs, wantDuration)
	}

	// Body is silent — assert at least some samples exist and they are zero.
	body := data[44:]
	for i, b := range body {
		if b != 0 {
			t.Errorf("non-zero PCM byte at offset %d: %d", i, b)
			break
		}
	}
}

func TestTTSClient_Synthesize_DurationScalesWithText(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	client := dryrun.NewTTSClient()

	short := filepath.Join(dir, "short.wav")
	long := filepath.Join(dir, "long.wav")

	shortResp, err := client.Synthesize(context.Background(), domain.TTSRequest{
		Text:       "짧은 문장.",
		Model:      "m",
		OutputPath: short,
	})
	if err != nil {
		t.Fatalf("short Synthesize: %v", err)
	}
	longResp, err := client.Synthesize(context.Background(), domain.TTSRequest{
		Text:       strings.Repeat("긴 문장입니다. ", 50),
		Model:      "m",
		OutputPath: long,
	})
	if err != nil {
		t.Fatalf("long Synthesize: %v", err)
	}

	if longResp.DurationMs <= shortResp.DurationMs {
		t.Errorf("long DurationMs %d should exceed short %d", longResp.DurationMs, shortResp.DurationMs)
	}
}

func TestTTSClient_Synthesize_DefaultsToWAVFormat(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out := filepath.Join(dir, "out.wav")

	client := dryrun.NewTTSClient()
	if _, err := client.Synthesize(context.Background(), domain.TTSRequest{
		Text:       "abc",
		Model:      "m",
		OutputPath: out,
		// Format omitted — should default to wav, not error.
	}); err != nil {
		t.Errorf("Synthesize with empty format: %v", err)
	}
}

func TestTTSClient_Synthesize_RejectsNonWAVFormat(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out := filepath.Join(dir, "out.mp3")

	client := dryrun.NewTTSClient()
	_, err := client.Synthesize(context.Background(), domain.TTSRequest{
		Text:       "abc",
		Model:      "m",
		OutputPath: out,
		Format:     "mp3",
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("err = %v, want ErrValidation", err)
	}
}

func TestTTSClient_Synthesize_ValidationErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out := filepath.Join(dir, "x.wav")
	client := dryrun.NewTTSClient()

	cases := []struct {
		name string
		req  domain.TTSRequest
	}{
		{"empty text", domain.TTSRequest{Model: "m", OutputPath: out}},
		{"empty path", domain.TTSRequest{Text: "t", Model: "m"}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := client.Synthesize(context.Background(), tc.req)
			if !errors.Is(err, domain.ErrValidation) {
				t.Errorf("err = %v, want ErrValidation", err)
			}
		})
	}
}
