package dryrun

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// WAV constants chosen to match the byte-rate the production duration
// estimator in dashscope/tts.go assumes (176_400 bytes/sec). Concretely:
//
//	sampleRate * blockAlign  =  44100 * (1 * 2)  =  88_200  ←  this is half
//	the constant, because the estimator is bytes/sec=176_400 — i.e. it
//	double-counts vs. the strict mono PCM spec. We mirror the *production
//	constant* exactly so dryrun.DurationMs == dashscope.DurationMs for
//	identical byte counts. Re-deriving from first principles would diverge.
const (
	wavSampleRate     uint32 = 44100
	wavBitsPerSample  uint16 = 16
	wavNumChannels    uint16 = 1
	wavBytesPerSecond        = 176_400 // mirrors dashscope/tts.go production constant
	wavHeaderSize            = 44
)

// secondsPerCodepoint approximates spoken duration for Korean narration
// (~7 codepoints/sec). This produces a placeholder WAV whose length is
// proportional to the input text, so Phase C ken_burns timing has a
// realistic value to compose against in dry-run.
const secondsPerCodepoint = 0.14

// TTSClient is a fake domain.TTSSynthesizer that writes a silent WAV to
// req.OutputPath. The WAV is a real, ffmpeg-readable file so the chunked
// concat path in tts_track.go works identically to production.
type TTSClient struct{}

// NewTTSClient constructs a dry-run TTSClient. Like ImageClient, no
// configuration is required: WAV format constants are fixed to match the
// production duration estimator exactly.
func NewTTSClient() *TTSClient {
	return &TTSClient{}
}

// Synthesize writes a silent WAV to req.OutputPath whose duration is sized
// from the input text length. Returns CostUSD=0 and Provider="dryrun".
//
// Format defaults to "wav" when empty (mirrors dashscope.TTSClient). Non-wav
// formats return ErrValidation — the dry-run client does not synthesize
// compressed audio because the production duration math assumes wav, and a
// silent mp3 would either need a real encoder or break the byte-rate
// assumption downstream.
func (c *TTSClient) Synthesize(_ context.Context, req domain.TTSRequest) (domain.TTSResponse, error) {
	if req.Text == "" {
		return domain.TTSResponse{}, fmt.Errorf("dryrun tts synthesize: %w: text is empty", domain.ErrValidation)
	}
	if req.OutputPath == "" {
		return domain.TTSResponse{}, fmt.Errorf("dryrun tts synthesize: %w: output path is empty", domain.ErrValidation)
	}
	format := req.Format
	if format == "" {
		format = "wav"
	}
	if format != "wav" {
		return domain.TTSResponse{}, fmt.Errorf("dryrun tts synthesize: %w: format %q unsupported (wav only)", domain.ErrValidation, format)
	}

	start := time.Now()

	dataBytes := dataBytesForText(req.Text)
	audio := encodeSilentWAV(dataBytes)

	if err := writeFileAtomic(req.OutputPath, audio); err != nil {
		return domain.TTSResponse{}, fmt.Errorf("dryrun tts: write wav: %w", err)
	}

	durationMs := int64(len(audio)) * 1000 / wavBytesPerSecond

	_ = start // duration is reported via the WAV byte-rate estimate, not wall clock
	return domain.TTSResponse{
		AudioPath:  req.OutputPath,
		DurationMs: durationMs,
		Model:      req.Model,
		Provider:   Provider,
		CostUSD:    0,
	}, nil
}

// dataBytesForText returns the byte count for the silent PCM body, sized
// from the input text's codepoint count. Block-aligned to keep the WAV
// valid (mono 16-bit = 2-byte frames; data section must be a multiple).
func dataBytesForText(text string) int {
	runes := len([]rune(text))
	seconds := math.Max(0.05, float64(runes)*secondsPerCodepoint) // 50ms floor: empty-ish input still yields a playable file
	bytes := int(seconds * float64(wavBytesPerSecond))
	blockAlign := int(wavNumChannels) * int(wavBitsPerSample) / 8
	if rem := bytes % blockAlign; rem != 0 {
		bytes += blockAlign - rem
	}
	return bytes
}

// encodeSilentWAV builds a 44-byte canonical RIFF/WAVE header followed by
// dataBytes zero-filled PCM samples. ffmpeg with `-c copy` requires
// identical codec params across concat inputs — keeping these constants
// fixed is what lets per-chunk synthesis + concat work in the dryrun path.
func encodeSilentWAV(dataBytes int) []byte {
	buf := bytes.NewBuffer(make([]byte, 0, wavHeaderSize+dataBytes))

	blockAlign := wavNumChannels * wavBitsPerSample / 8
	byteRate := wavSampleRate * uint32(blockAlign)

	// RIFF chunk descriptor
	buf.WriteString("RIFF")
	_ = binary.Write(buf, binary.LittleEndian, uint32(36+dataBytes))
	buf.WriteString("WAVE")

	// fmt sub-chunk
	buf.WriteString("fmt ")
	_ = binary.Write(buf, binary.LittleEndian, uint32(16)) // PCM fmt chunk size
	_ = binary.Write(buf, binary.LittleEndian, uint16(1))  // AudioFormat = 1 (PCM)
	_ = binary.Write(buf, binary.LittleEndian, wavNumChannels)
	_ = binary.Write(buf, binary.LittleEndian, wavSampleRate)
	_ = binary.Write(buf, binary.LittleEndian, byteRate)
	_ = binary.Write(buf, binary.LittleEndian, blockAlign)
	_ = binary.Write(buf, binary.LittleEndian, wavBitsPerSample)

	// data sub-chunk
	buf.WriteString("data")
	_ = binary.Write(buf, binary.LittleEndian, uint32(dataBytes))

	// Silent PCM body — zeros
	if dataBytes > 0 {
		buf.Write(make([]byte, dataBytes))
	}

	return buf.Bytes()
}
