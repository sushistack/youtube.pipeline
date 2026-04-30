package comfyui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// jsonResponseLimit caps decoded JSON payloads from /prompt and /history.
// /history responses can be a few KiB after a successful run; 1 MiB is
// generous and matches the dashscope client convention.
const jsonResponseLimit = 1 << 20

// promptResponse is the /prompt acknowledgement carrying the queued ID.
type promptResponse struct {
	PromptID  string         `json:"prompt_id"`
	Number    int            `json:"number,omitempty"`
	NodeErrors map[string]any `json:"node_errors,omitempty"`
}

// historyResponse is the /history/{prompt_id} envelope. ComfyUI returns the
// prompt_id as the top-level key. PENDING is signalled by an empty body or
// `status.completed:false`. The `outputs.<node_id>.images` array carries the
// rendered file metadata once the workflow finishes.
type historyResponse map[string]historyEntry

type historyEntry struct {
	Outputs map[string]historyOutput `json:"outputs"`
	Status  *historyStatus           `json:"status,omitempty"`
}

type historyOutput struct {
	Images []historyImage `json:"images,omitempty"`
}

type historyImage struct {
	Filename  string `json:"filename"`
	Subfolder string `json:"subfolder"`
	Type      string `json:"type"`
}

type historyStatus struct {
	StatusStr string             `json:"status_str"`
	Completed bool               `json:"completed"`
	Messages  [][]json.RawMessage `json:"messages,omitempty"`
}

type uploadResponse struct {
	Name      string `json:"name"`
	Subfolder string `json:"subfolder"`
	Type      string `json:"type"`
}

// submitPrompt POSTs the prepared workflow + client_id and returns the
// prompt_id assigned by the queue.
func submitPrompt(
	ctx context.Context,
	httpClient *http.Client,
	endpoint, clientID string,
	workflow []byte,
) (string, error) {
	body := map[string]any{
		"prompt":    json.RawMessage(workflow),
		"client_id": clientID,
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("comfyui submit: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/prompt", bytes.NewReader(encoded))
	if err != nil {
		return "", fmt.Errorf("comfyui submit: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("comfyui submit: %w: %v", domain.ErrStageFailed, err)
	}
	defer resp.Body.Close()

	if err := classifyStatus("submit", resp); err != nil {
		return "", err
	}

	var payload promptResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, jsonResponseLimit)).Decode(&payload); err != nil {
		return "", fmt.Errorf("comfyui submit: %w: decode: %v", domain.ErrStageFailed, err)
	}
	if payload.PromptID == "" {
		return "", fmt.Errorf("comfyui submit: %w: response missing prompt_id", domain.ErrStageFailed)
	}
	// ComfyUI returns 200 + node_errors when the workflow validates as queueable
	// but contains graph-level issues (unsatisfiable inputs, missing files).
	// Without this check the job sits in the queue and we burn the full 300s
	// polling cap before failing as ErrUpstreamTimeout. Surface as ErrValidation
	// up front so the caller doesn't retry a workflow that can't ever succeed.
	if len(payload.NodeErrors) > 0 {
		nodeIDs := make([]string, 0, len(payload.NodeErrors))
		for id := range payload.NodeErrors {
			nodeIDs = append(nodeIDs, id)
		}
		return "", fmt.Errorf("comfyui submit: %w: node_errors on nodes %v (prompt_id=%s)",
			domain.ErrValidation, nodeIDs, payload.PromptID)
	}
	return payload.PromptID, nil
}

// fetchHistory polls /history/{prompt_id}. It returns (entry, present, err)
// where `present` is false when the queue has not yet emitted a record (empty
// body) or is still working (completed:false).
func fetchHistory(
	ctx context.Context,
	httpClient *http.Client,
	endpoint, promptID string,
) (historyEntry, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/history/"+promptID, nil)
	if err != nil {
		return historyEntry{}, false, fmt.Errorf("comfyui history: create request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return historyEntry{}, false, fmt.Errorf("comfyui history: %w: %v", domain.ErrStageFailed, err)
	}
	defer resp.Body.Close()

	if err := classifyStatus("history", resp); err != nil {
		return historyEntry{}, false, err
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, jsonResponseLimit))
	if err != nil {
		return historyEntry{}, false, fmt.Errorf("comfyui history: %w: read: %v", domain.ErrStageFailed, err)
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("{}")) {
		return historyEntry{}, false, nil
	}

	var hist historyResponse
	if err := json.Unmarshal(trimmed, &hist); err != nil {
		return historyEntry{}, false, fmt.Errorf("comfyui history: %w: decode: %v", domain.ErrStageFailed, err)
	}
	entry, ok := hist[promptID]
	if !ok {
		return historyEntry{}, false, nil
	}
	if entry.Status != nil && !entry.Status.Completed && entry.Status.StatusStr != "error" {
		return historyEntry{}, false, nil
	}
	return entry, true, nil
}

// downloadView fetches the rendered image bytes from /view. The size cap is
// applied via the limit + 1 trick so callers can distinguish "exactly at cap"
// from "exceeded cap".
func downloadView(
	ctx context.Context,
	httpClient *http.Client,
	endpoint string,
	img historyImage,
	limit int,
) ([]byte, error) {
	url := fmt.Sprintf("%s/view?filename=%s&subfolder=%s&type=%s",
		endpoint,
		queryEscape(img.Filename),
		queryEscape(img.Subfolder),
		queryEscape(img.Type),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("comfyui view: create request: %w", err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("comfyui view: %w: %v", domain.ErrStageFailed, err)
	}
	defer resp.Body.Close()
	if err := classifyStatus("view", resp); err != nil {
		return nil, err
	}
	imageBytes, err := io.ReadAll(io.LimitReader(resp.Body, int64(limit)+1))
	if err != nil {
		return nil, fmt.Errorf("comfyui view: %w: read: %v", domain.ErrStageFailed, err)
	}
	if len(imageBytes) > limit {
		return nil, fmt.Errorf("comfyui view: %w: image exceeds %d byte cap", domain.ErrValidation, limit)
	}
	return imageBytes, nil
}

// uploadImage POSTs a multipart form to /upload/image with field `image`.
// Returns the filename ComfyUI assigned (used in LoadImage substitution).
func uploadImage(
	ctx context.Context,
	httpClient *http.Client,
	endpoint, filename string,
	contentType string,
	data []byte,
) (string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	header := make(map[string][]string)
	header["Content-Disposition"] = []string{
		fmt.Sprintf(`form-data; name="image"; filename=%q`, filename),
	}
	if contentType != "" {
		header["Content-Type"] = []string{contentType}
	}
	part, err := writer.CreatePart(header)
	if err != nil {
		return "", fmt.Errorf("comfyui upload: create part: %w", err)
	}
	if _, err := part.Write(data); err != nil {
		return "", fmt.Errorf("comfyui upload: write part: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("comfyui upload: close writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/upload/image", &body)
	if err != nil {
		return "", fmt.Errorf("comfyui upload: create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("comfyui upload: %w: %v", domain.ErrStageFailed, err)
	}
	defer resp.Body.Close()
	if err := classifyStatus("upload", resp); err != nil {
		return "", err
	}
	var payload uploadResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, jsonResponseLimit)).Decode(&payload); err != nil {
		return "", fmt.Errorf("comfyui upload: %w: decode: %v", domain.ErrStageFailed, err)
	}
	if payload.Name == "" {
		return "", fmt.Errorf("comfyui upload: %w: response missing name", domain.ErrStageFailed)
	}
	return payload.Name, nil
}

// classifyStatus maps an HTTP response to nil or a canonical domain error.
func classifyStatus(phase string, resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return fmt.Errorf("comfyui %s: %w: status %d: %s", phase, domain.ErrRateLimited, resp.StatusCode, body)
	case resp.StatusCode >= 500:
		return fmt.Errorf("comfyui %s: %w: status %d: %s", phase, domain.ErrStageFailed, resp.StatusCode, body)
	default:
		return fmt.Errorf("comfyui %s: %w: status %d: %s", phase, domain.ErrValidation, resp.StatusCode, body)
	}
}

// queryEscape is a thin alias matching net/url's QueryEscape but inline so the
// caller does not need to import net/url just for path construction. The
// fields are short identifiers controlled by the upstream and never include
// hostile characters in normal operation, but encoding is still applied for
// safety.
func queryEscape(s string) string {
	// Minimal-allocation inline escape — matches net/url.QueryEscape semantics
	// for the subset of bytes ComfyUI emits in filename/subfolder/type fields.
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case 'a' <= c && c <= 'z',
			'A' <= c && c <= 'Z',
			'0' <= c && c <= '9',
			c == '-' || c == '_' || c == '.' || c == '~':
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0xF])
		}
	}
	return b.String()
}
