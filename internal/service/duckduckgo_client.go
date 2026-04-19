package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"time"
)

var vqdPatterns = []*regexp.Regexp{
	regexp.MustCompile(`vqd=([0-9-]+)\&`),
	regexp.MustCompile(`"vqd":"([0-9-]+)"`),
	regexp.MustCompile(`vqd='([0-9-]+)'`),
}

// DuckDuckGoClient is the default production image-search client.
type DuckDuckGoClient struct {
	httpClient *http.Client
}

func NewDuckDuckGoClient(httpClient *http.Client) *DuckDuckGoClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &DuckDuckGoClient{httpClient: httpClient}
}

func (c *DuckDuckGoClient) SearchImages(ctx context.Context, query string, limit int) ([]CharacterSearchResult, error) {
	vqd, err := c.fetchVQD(ctx, query)
	if err != nil {
		return nil, err
	}

	params := url.Values{}
	params.Set("q", query)
	params.Set("o", "json")
	params.Set("l", "wt-wt")
	params.Set("p", "-1")
	params.Set("vqd", vqd)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://duckduckgo.com/i.js?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("ddg image request: %w", err)
	}
	req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	req.Header.Set("Referer", "https://duckduckgo.com/")
	req.Header.Set("User-Agent", defaultDuckDuckGoUserAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ddg image request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("ddg image request: status %d: %s", resp.StatusCode, string(body))
	}

	var payload struct {
		Results []struct {
			Image     string `json:"image"`
			URL       string `json:"url"`
			Thumbnail string `json:"thumbnail"`
			Title     string `json:"title"`
			Source    string `json:"source"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("ddg image response decode: %w", err)
	}

	if limit > 0 && len(payload.Results) > limit {
		payload.Results = payload.Results[:limit]
	}
	results := make([]CharacterSearchResult, 0, len(payload.Results))
	for _, item := range payload.Results {
		results = append(results, CharacterSearchResult{
			PageURL:     item.URL,
			ImageURL:    item.Image,
			PreviewURL:  item.Thumbnail,
			Title:       item.Title,
			SourceLabel: item.Source,
		})
	}
	return results, nil
}

const defaultDuckDuckGoUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/135.0.0.0 Safari/537.36"

func (c *DuckDuckGoClient) fetchVQD(ctx context.Context, query string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://duckduckgo.com/?q="+url.QueryEscape(query), nil)
	if err != nil {
		return "", fmt.Errorf("ddg token request: %w", err)
	}
	req.Header.Set("User-Agent", defaultDuckDuckGoUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("ddg token request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return "", fmt.Errorf("ddg token request: read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ddg token request: status %d", resp.StatusCode)
	}
	for _, pattern := range vqdPatterns {
		matches := pattern.FindSubmatch(body)
		if len(matches) == 2 {
			return string(matches[1]), nil
		}
	}
	return "", fmt.Errorf("ddg token request: vqd token not found")
}
