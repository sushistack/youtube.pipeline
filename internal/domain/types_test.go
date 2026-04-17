package domain

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestAllStages_ExactlyFifteen(t *testing.T) {
	if got := len(AllStages()); got != 15 {
		t.Errorf("len(AllStages()) = %d, want 15", got)
	}
}

func TestAllStages_Values(t *testing.T) {
	expected := []Stage{
		"pending", "research", "structure", "write", "visual_break",
		"review", "critic", "scenario_review", "character_pick",
		"image", "tts", "batch_review", "assemble", "metadata_ack", "complete",
	}
	for i, want := range expected {
		if AllStages()[i] != want {
			t.Errorf("AllStages()[%d] = %q, want %q", i, AllStages()[i], want)
		}
	}
}

func TestRun_JSONTags_SnakeCase(t *testing.T) {
	assertSnakeCaseJSONTags(t, reflect.TypeOf(Run{}))
}

func TestEpisode_JSONTags_SnakeCase(t *testing.T) {
	assertSnakeCaseJSONTags(t, reflect.TypeOf(Episode{}))
}

func TestShot_JSONTags_SnakeCase(t *testing.T) {
	assertSnakeCaseJSONTags(t, reflect.TypeOf(Shot{}))
}

func TestDecision_JSONTags_SnakeCase(t *testing.T) {
	assertSnakeCaseJSONTags(t, reflect.TypeOf(Decision{}))
}

func TestNormalizedResponse_JSONTags_SnakeCase(t *testing.T) {
	assertSnakeCaseJSONTags(t, reflect.TypeOf(NormalizedResponse{}))
}

func TestRun_JSONRoundTrip(t *testing.T) {
	r := Run{
		ID:    "scp-049-run-1",
		SCPID: "049",
		Stage: StagePending,
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if _, ok := m["scp_id"]; !ok {
		t.Error("expected JSON key 'scp_id', not found")
	}
}

func TestStage_IsValid(t *testing.T) {
	for _, s := range AllStages() {
		if !s.IsValid() {
			t.Errorf("Stage(%q).IsValid() = false, want true", s)
		}
	}
	invalid := []Stage{"", "garbage", "PENDING", "research ", " write"}
	for _, s := range invalid {
		if s.IsValid() {
			t.Errorf("Stage(%q).IsValid() = true, want false", s)
		}
	}
}

func TestAllEvents_ExactlyFour(t *testing.T) {
	if got := len(AllEvents()); got != 4 {
		t.Errorf("len(AllEvents()) = %d, want 4", got)
	}
}

func TestAllEvents_Values(t *testing.T) {
	expected := []Event{"start", "complete", "approve", "retry"}
	for i, want := range expected {
		if AllEvents()[i] != want {
			t.Errorf("AllEvents()[%d] = %q, want %q", i, AllEvents()[i], want)
		}
	}
}

func TestEvent_IsValid(t *testing.T) {
	for _, e := range AllEvents() {
		if !e.IsValid() {
			t.Errorf("Event(%q).IsValid() = false, want true", e)
		}
	}
	invalid := []Event{"", "unknown", "START", "cancel"}
	for _, e := range invalid {
		if e.IsValid() {
			t.Errorf("Event(%q).IsValid() = true, want false", e)
		}
	}
}

func TestAllStatuses_ExactlySix(t *testing.T) {
	if got := len(AllStatuses()); got != 6 {
		t.Errorf("len(AllStatuses()) = %d, want 6", got)
	}
}

func TestAllStatuses_Values(t *testing.T) {
	expected := []Status{"pending", "running", "waiting", "completed", "failed", "cancelled"}
	for i, want := range expected {
		if AllStatuses()[i] != want {
			t.Errorf("AllStatuses()[%d] = %q, want %q", i, AllStatuses()[i], want)
		}
	}
}

func TestStatus_IsValid(t *testing.T) {
	for _, s := range AllStatuses() {
		if !s.IsValid() {
			t.Errorf("Status(%q).IsValid() = false, want true", s)
		}
	}
	invalid := []Status{"", "unknown", "RUNNING", "done"}
	for _, s := range invalid {
		if s.IsValid() {
			t.Errorf("Status(%q).IsValid() = true, want false", s)
		}
	}
}

func assertSnakeCaseJSONTags(t *testing.T, rt reflect.Type) {
	t.Helper()
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		tag := f.Tag.Get("json")
		if tag == "" || tag == "-" {
			t.Errorf("field %s has no json tag", f.Name)
			continue
		}
		name := strings.Split(tag, ",")[0]
		if name != strings.ToLower(name) {
			t.Errorf("field %s json tag %q is not lowercase", f.Name, name)
		}
		if strings.Contains(name, "-") {
			t.Errorf("field %s json tag %q uses hyphens, want snake_case", f.Name, name)
		}
	}
}
