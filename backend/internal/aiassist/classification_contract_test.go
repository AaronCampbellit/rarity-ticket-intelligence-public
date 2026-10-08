package aiassist

import (
	"errors"
	"strings"
	"testing"
)

func TestParseClassificationOutputAcceptsOnlyAuthorizedStructuredSuggestions(t *testing.T) {
	got, err := ParseClassificationOutput([]byte(`{"suggestions":[{"tag_id":"vpn","confidence":0.975,"rationale":"VPN outage details"}]}`), []string{"vpn"})
	if err != nil {
		t.Fatalf("ParseClassificationOutput() error = %v", err)
	}
	if len(got) != 1 || got[0].TagID != "vpn" || got[0].Confidence != 0.975 || got[0].Rationale != "VPN outage details" {
		t.Fatalf("suggestions = %+v", got)
	}
}

func TestParseClassificationOutputPreservesExplicitEmptySuggestions(t *testing.T) {
	got, err := ParseClassificationOutput([]byte(`{"suggestions":[]}`), []string{"vpn"})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("got %#v, want a non-nil empty result", got)
	}
}

func TestParseClassificationOutputRejectsUnknownFieldsCandidatesAndUnsafeBounds(t *testing.T) {
	for _, body := range [][]byte{
		[]byte(`{"suggestions":[],"extra":true}`),
		[]byte(`{"suggestions":[{"tag_id":"not-authorized","confidence":0.9,"rationale":"x"}]}`),
		[]byte(`{"suggestions":[{"tag_id":"vpn","confidence":1.1,"rationale":"x"}]}`),
		[]byte(`{"suggestions":[{"tag_id":"vpn","confidence":0.9,"rationale":"` + string(make([]byte, 2049)) + `"}]}`),
	} {
		if _, err := ParseClassificationOutput(body, []string{"vpn"}); !errors.Is(err, ErrInvalidAIOutput) {
			t.Fatalf("ParseClassificationOutput(%q) error = %v, want ErrInvalidAIOutput", body, err)
		}
	}
}

func TestPrepareProviderRequestUsesDedicatedClassificationInstruction(t *testing.T) {
	model := ModelProfile{ProviderModelID: "model", ContextLimit: 1000, OutputLimit: 100, Enabled: true}
	body, err := PrepareProviderRequest(AdapterOpenAICompatible, model, ProviderRequest{Feature: FeatureClassification, Fields: []ContextField{{Name: "title", Value: "VPN outage", Classification: ContextStandard}}, AuthorizedCandidateIDs: []string{"vpn"}, MaxOutputUnits: 100})
	if err != nil || !strings.Contains(string(body), "tag_id") || strings.Contains(string(body), "only text, confidence, and candidate_ids") {
		t.Fatalf("classification body=%s error=%v", body, err)
	}
}
