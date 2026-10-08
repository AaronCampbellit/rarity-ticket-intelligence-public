// Package capacity evaluates release evidence against the accepted V1
// capacity and latency contract.
package capacity

import (
	"math"
	"sort"
	"strings"
)

type Evidence struct {
	SchemaVersion          int       `json:"schema_version"`
	Revision               string    `json:"revision"`
	ConcurrentTechnicians  int       `json:"concurrent_technicians"`
	ClientOrganizations    int       `json:"client_organizations"`
	PeakDayNewTickets      int       `json:"peak_day_new_tickets"`
	BurstSent              int       `json:"burst_sent"`
	BurstDurable           int       `json:"burst_durable"`
	AvailabilityPercent    float64   `json:"availability_percent"`
	StorageHeadroomPercent float64   `json:"storage_headroom_percent"`
	ReadLatencyMS          []float64 `json:"read_latency_ms"`
	MutationLatencyMS      []float64 `json:"mutation_latency_ms"`
	DashboardLatencyMS     []float64 `json:"dashboard_latency_ms"`
	SearchLatencyMS        []float64 `json:"search_latency_ms"`
	APIAcknowledgementMS   []float64 `json:"api_acknowledgement_ms"`
	EventProcessingSeconds []float64 `json:"event_processing_seconds"`
	SearchIndexingSeconds  []float64 `json:"search_indexing_seconds"`
	GraphProcessingSeconds []float64 `json:"graph_processing_seconds"`
	DattoFreshnessSeconds  []float64 `json:"datto_freshness_seconds"`
}

type Measurement struct {
	Name   string  `json:"name"`
	Actual float64 `json:"actual"`
	Target float64 `json:"target"`
	Unit   string  `json:"unit"`
}

type Failure struct {
	Measurement string  `json:"measurement"`
	Actual      float64 `json:"actual"`
	Target      float64 `json:"target"`
	Rule        string  `json:"rule"`
}

type Report struct {
	SchemaVersion int           `json:"schema_version"`
	Revision      string        `json:"revision"`
	Passed        bool          `json:"passed"`
	Measurements  []Measurement `json:"measurements"`
	Failures      []Failure     `json:"failures"`
}

func Evaluate(evidence Evidence) Report {
	report := Report{SchemaVersion: 1, Revision: evidence.Revision}
	atLeast(&report, "schema_version", float64(evidence.SchemaVersion), 1, "version")
	if strings.TrimSpace(evidence.Revision) == "" {
		report.Failures = append(report.Failures, Failure{
			Measurement: "revision",
			Rule:        "required",
		})
	}
	atLeast(&report, "concurrent_technicians", float64(evidence.ConcurrentTechnicians), 50, "count")
	atLeast(&report, "client_organizations", float64(evidence.ClientOrganizations), 1000, "count")
	atLeast(&report, "peak_day_new_tickets", float64(evidence.PeakDayNewTickets), 5000, "count")
	atLeast(&report, "inbound_burst_sent", float64(evidence.BurstSent), 1000, "events")
	exactly(&report, "inbound_burst_durable", float64(evidence.BurstDurable), float64(evidence.BurstSent), "events")
	atLeast(&report, "availability_percent", evidence.AvailabilityPercent, 99.9, "percent")
	atLeast(&report, "storage_headroom_percent", evidence.StorageHeadroomPercent, 30, "percent")

	atMostPercentile(&report, "interactive_reads_p95_ms", evidence.ReadLatencyMS, 0.95, 500, "milliseconds")
	atMostPercentile(&report, "interactive_reads_p99_ms", evidence.ReadLatencyMS, 0.99, 1500, "milliseconds")
	atMostPercentile(&report, "mutations_p95_ms", evidence.MutationLatencyMS, 0.95, 1000, "milliseconds")
	atMostPercentile(&report, "mutations_p99_ms", evidence.MutationLatencyMS, 0.99, 2000, "milliseconds")
	atMostPercentile(&report, "dashboards_p95_ms", evidence.DashboardLatencyMS, 0.95, 2000, "milliseconds")
	atMostPercentile(&report, "search_p95_ms", evidence.SearchLatencyMS, 0.95, 1500, "milliseconds")
	atMostPercentile(&report, "api_acknowledgement_p95_ms", evidence.APIAcknowledgementMS, 0.95, 500, "milliseconds")
	atMostPercentile(&report, "event_processing_p95_seconds", evidence.EventProcessingSeconds, 0.95, 60, "seconds")
	atMostPercentile(&report, "search_indexing_p95_seconds", evidence.SearchIndexingSeconds, 0.95, 60, "seconds")
	atMostPercentile(&report, "graph_processing_p95_seconds", evidence.GraphProcessingSeconds, 0.95, 60, "seconds")
	atMostPercentile(&report, "datto_freshness_p95_seconds", evidence.DattoFreshnessSeconds, 0.95, 900, "seconds")

	report.Passed = len(report.Failures) == 0
	return report
}

func atLeast(report *Report, name string, actual, target float64, unit string) {
	record(report, name, actual, target, unit, actual >= target, "at_least")
}

func exactly(report *Report, name string, actual, target float64, unit string) {
	record(report, name, actual, target, unit, actual == target, "exactly")
}

func atMostPercentile(
	report *Report,
	name string,
	samples []float64,
	percentileValue float64,
	target float64,
	unit string,
) {
	actual, ok := percentile(samples, percentileValue)
	record(report, name, actual, target, unit, ok && actual <= target, "at_most")
}

func record(
	report *Report,
	name string,
	actual, target float64,
	unit string,
	passed bool,
	rule string,
) {
	report.Measurements = append(report.Measurements, Measurement{
		Name: name, Actual: actual, Target: target, Unit: unit,
	})
	if !passed || math.IsNaN(actual) || math.IsInf(actual, 0) {
		report.Failures = append(report.Failures, Failure{
			Measurement: name,
			Actual:      actual,
			Target:      target,
			Rule:        rule,
		})
	}
}

func percentile(samples []float64, requested float64) (float64, bool) {
	if len(samples) == 0 {
		return 0, false
	}
	values := append([]float64(nil), samples...)
	for _, value := range values {
		if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			return value, false
		}
	}
	sort.Float64s(values)
	index := int(math.Ceil(requested*float64(len(values)))) - 1
	if index < 0 {
		index = 0
	}
	return values[index], true
}
