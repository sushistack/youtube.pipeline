package domain

import "context"

// TextRequest contains parameters for text generation.
type TextRequest struct {
	Prompt      string  `json:"prompt"`
	Model       string  `json:"model"`
	MaxTokens   int     `json:"max_tokens"`
	Temperature float64 `json:"temperature"`
}

// TextResponse wraps a normalized text generation response.
type TextResponse struct {
	NormalizedResponse
}

// ImageRequest contains parameters for image generation.
type ImageRequest struct {
	Prompt     string `json:"prompt"`
	Model      string `json:"model"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	OutputPath string `json:"output_path,omitempty"`
}

// ImageEditRequest contains parameters for image editing with a reference.
type ImageEditRequest struct {
	Prompt             string `json:"prompt"`
	Model              string `json:"model"`
	ReferenceImageURL string `json:"reference_image_url"`
	Width              int    `json:"width"`
	Height             int    `json:"height"`
	OutputPath         string `json:"output_path,omitempty"`
}

// ImageResponse contains the result of an image generation or edit.
type ImageResponse struct {
	ImagePath  string  `json:"image_path"`
	Model      string  `json:"model"`
	Provider   string  `json:"provider"`
	CostUSD    float64 `json:"cost_usd"`
	DurationMs int64   `json:"duration_ms"`
}

// TTSRequest contains parameters for text-to-speech synthesis.
// OutputPath is the absolute file path the caller wants the audio written to;
// the client writes the bytes there and the TTS track owns directory creation.
// Format selects the audio codec (e.g. "wav", "mp3"); defaults to "wav" when empty.
type TTSRequest struct {
	Text       string `json:"text"`
	Model      string `json:"model"`
	Voice      string `json:"voice"`
	OutputPath string `json:"output_path,omitempty"`
	Format     string `json:"format,omitempty"`
}

// TTSResponse contains the result of TTS synthesis.
type TTSResponse struct {
	AudioPath  string  `json:"audio_path"`
	DurationMs int64   `json:"duration_ms"`
	Model      string  `json:"model"`
	Provider   string  `json:"provider"`
	CostUSD    float64 `json:"cost_usd"`
}

// TextGenerator produces text from a prompt via an LLM provider.
// Implementations must accept *http.Client via constructor — never use http.DefaultClient.
type TextGenerator interface {
	Generate(ctx context.Context, req TextRequest) (TextResponse, error)
}

// ImageGenerator produces or edits images via an LLM provider.
// Implementations must accept *http.Client via constructor — never use http.DefaultClient.
type ImageGenerator interface {
	Generate(ctx context.Context, req ImageRequest) (ImageResponse, error)
	Edit(ctx context.Context, req ImageEditRequest) (ImageResponse, error)
}

// TTSSynthesizer converts text to speech audio via an LLM provider.
// Implementations must accept *http.Client via constructor — never use http.DefaultClient.
type TTSSynthesizer interface {
	Synthesize(ctx context.Context, req TTSRequest) (TTSResponse, error)
}
