package domain

import (
	"encoding/json"
	"testing"
)

func TestMetadataBundle_JSONRoundTrip(t *testing.T) {
	bundle := MetadataBundle{
		Version:     1,
		GeneratedAt: "2026-04-22T09:00:00Z",
		RunID:       "scp-049-run-1",
		SCPID:       "049",
		Title:       "SCP-049 - The Plague Doctor",
		AIGenerated: AIGeneratedFlags{Narration: true, Imagery: true, TTS: true},
		ModelsUsed: map[string]ModelRecord{
			"writer":           {Provider: "deepseek", Model: "deepseek-chat"},
			"critic":           {Provider: "gemini", Model: "gemini-2.0-flash"},
			"image":            {Provider: "dashscope", Model: "qwen-max-vl"},
			"tts":              {Provider: "dashscope", Model: "qwen3-tts-flash-2025-09-18", Voice: "longhua"},
			"visual_breakdown": {Provider: "gemini", Model: "gemini-2.0-flash"},
		},
	}

	data, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent: %v", err)
	}

	var got MetadataBundle
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.Version != 1 {
		t.Errorf("Version = %d, want 1", got.Version)
	}
	if got.RunID != "scp-049-run-1" {
		t.Errorf("RunID = %q, want %q", got.RunID, "scp-049-run-1")
	}
	if !got.AIGenerated.Narration || !got.AIGenerated.Imagery || !got.AIGenerated.TTS {
		t.Error("AIGenerated flags not all true")
	}

	// Verify all 5 ModelsUsed keys present
	expectedKeys := []string{"writer", "critic", "image", "tts", "visual_breakdown"}
	for _, k := range expectedKeys {
		rec, ok := got.ModelsUsed[k]
		if !ok {
			t.Errorf("ModelsUsed missing key %q", k)
			continue
		}
		if rec.Provider == "" {
			t.Errorf("ModelsUsed[%q].Provider is empty", k)
		}
		if rec.Model == "" {
			t.Errorf("ModelsUsed[%q].Model is empty", k)
		}
	}

	// Verify TTS voice serialized
	ttsRec := got.ModelsUsed["tts"]
	if ttsRec.Voice != "longhua" {
		t.Errorf("ModelsUsed[tts].Voice = %q, want %q", ttsRec.Voice, "longhua")
	}
}

func TestSourceManifest_JSONRoundTrip(t *testing.T) {
	manifest := SourceManifest{
		Version:     1,
		GeneratedAt: "2026-04-22T09:00:00Z",
		RunID:       "scp-049-run-1",
		SCPID:       "049",
		SourceURL:   "https://scp-wiki.wikidot.com/scp-049",
		AuthorName:  "Djoric",
		License:     LicenseCCBYSA30,
		LicenseURL:  LicenseURLCCBYSA30,
		LicenseChain: []LicenseEntry{
			{
				Component:  "SCP article text",
				SourceURL:  "https://scp-wiki.wikidot.com/scp-049",
				AuthorName: "Djoric",
				License:    LicenseCCBYSA30,
			},
		},
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent: %v", err)
	}

	var got SourceManifest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.Version != 1 {
		t.Errorf("Version = %d, want 1", got.Version)
	}
	if got.License != LicenseCCBYSA30 {
		t.Errorf("License = %q, want %q", got.License, LicenseCCBYSA30)
	}
	if got.LicenseURL != LicenseURLCCBYSA30 {
		t.Errorf("LicenseURL = %q, want %q", got.LicenseURL, LicenseURLCCBYSA30)
	}
	if len(got.LicenseChain) != 1 {
		t.Fatalf("LicenseChain length = %d, want 1", len(got.LicenseChain))
	}
	if got.LicenseChain[0].Component != "SCP article text" {
		t.Errorf("LicenseChain[0].Component = %q, want %q", got.LicenseChain[0].Component, "SCP article text")
	}
	if got.LicenseChain[0].AuthorName != "Djoric" {
		t.Errorf("LicenseChain[0].AuthorName = %q, want %q", got.LicenseChain[0].AuthorName, "Djoric")
	}
}

func TestMetadataBundle_ModelsUsedAllFiveKeys(t *testing.T) {
	bundle := MetadataBundle{
		ModelsUsed: map[string]ModelRecord{
			"writer":           {Provider: "a", Model: "b"},
			"critic":           {Provider: "c", Model: "d"},
			"image":            {Provider: "e", Model: "f"},
			"tts":              {Provider: "g", Model: "h"},
			"visual_breakdown": {Provider: "i", Model: "j"},
		},
	}

	data, err := json.Marshal(bundle)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got MetadataBundle
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if len(got.ModelsUsed) != 5 {
		t.Errorf("ModelsUsed has %d keys, want 5", len(got.ModelsUsed))
	}
}

func TestLicenseChain_EmptySlice(t *testing.T) {
	manifest := SourceManifest{
		Version:      1,
		GeneratedAt:  "2026-04-22T09:00:00Z",
		RunID:        "test-run",
		SCPID:        "test",
		SourceURL:    "https://example.com",
		AuthorName:   "Author",
		License:      LicenseCCBYSA30,
		LicenseURL:   LicenseURLCCBYSA30,
		LicenseChain: []LicenseEntry{},
	}

	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got SourceManifest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.LicenseChain == nil {
		t.Error("LicenseChain is nil after round-trip, want empty slice")
	}
	if len(got.LicenseChain) != 0 {
		t.Errorf("LicenseChain length = %d, want 0", len(got.LicenseChain))
	}
}
