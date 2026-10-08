package capacity

import (
	"strings"
	"testing"
)

func TestEvaluateAcceptsEvidenceWithinV1Budgets(t *testing.T) {
	evidence := passingEvidence()

	report := Evaluate(evidence)

	if !report.Passed || len(report.Failures) != 0 {
		t.Fatalf("expected passing capacity report: %+v", report)
	}
	if len(report.Measurements) == 0 {
		t.Fatal("expected computed measurements")
	}
}

func TestEvaluateRejectsScaleLatencyFreshnessAndEventLoss(t *testing.T) {
	evidence := passingEvidence()
	evidence.ConcurrentTechnicians = 49
	evidence.ReadLatencyMS = append(repeated(100, 98), 2000, 2000)
	evidence.BurstDurable = 999
	evidence.EventProcessingSeconds = repeated(70, 100)
	evidence.StorageHeadroomPercent = 29.9

	report := Evaluate(evidence)

	if report.Passed {
		t.Fatal("expected failed capacity report")
	}
	for _, expected := range []string{
		"concurrent_technicians",
		"interactive_reads_p99_ms",
		"inbound_burst_durable",
		"event_processing_p95_seconds",
		"storage_headroom_percent",
	} {
		if !containsFailure(report.Failures, expected) {
			t.Fatalf("expected failure for %s: %+v", expected, report.Failures)
		}
	}
}

func TestEvaluateRejectsMissingSamples(t *testing.T) {
	evidence := passingEvidence()
	evidence.SearchLatencyMS = nil

	report := Evaluate(evidence)

	if report.Passed || !containsFailure(report.Failures, "search_p95_ms") {
		t.Fatalf("expected missing search sample failure: %+v", report)
	}
}

func passingEvidence() Evidence {
	return Evidence{
		SchemaVersion:          1,
		Revision:               "revision-123",
		ConcurrentTechnicians:  50,
		ClientOrganizations:    1000,
		PeakDayNewTickets:      5000,
		BurstSent:              1000,
		BurstDurable:           1000,
		AvailabilityPercent:    99.95,
		StorageHeadroomPercent: 35,
		ReadLatencyMS:          repeated(400, 100),
		MutationLatencyMS:      repeated(800, 100),
		DashboardLatencyMS:     repeated(1500, 100),
		SearchLatencyMS:        repeated(1200, 100),
		APIAcknowledgementMS:   repeated(400, 100),
		EventProcessingSeconds: repeated(50, 100),
		SearchIndexingSeconds:  repeated(50, 100),
		GraphProcessingSeconds: repeated(50, 100),
		DattoFreshnessSeconds:  repeated(14*60, 100),
	}
}

func repeated(value float64, count int) []float64 {
	values := make([]float64, count)
	for index := range values {
		values[index] = value
	}
	return values
}

func containsFailure(failures []Failure, name string) bool {
	for _, failure := range failures {
		if strings.Contains(failure.Measurement, name) {
			return true
		}
	}
	return false
}
