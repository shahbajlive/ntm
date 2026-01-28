package ensemble

import (
	"math"
	"strings"
	"testing"
)

func TestVelocityTracker_DedupFindings(t *testing.T) {
	tracker := NewVelocityTracker()
	output := ModeOutput{
		ModeID: "formal",
		TopFindings: []Finding{
			{Finding: "Cache results.", Impact: ImpactLow, Confidence: 0.5, EvidencePointer: "file.go:10"},
			{Finding: "cache results", Impact: ImpactLow, Confidence: 0.5, EvidencePointer: "file.go:10"},
			{Finding: "Cache results", Impact: ImpactLow, Confidence: 0.5},
		},
	}

	tracker.RecordOutput("formal", output, 1000)

	if len(tracker.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(tracker.Entries))
	}
	entry := tracker.Entries[0]
	if entry.FindingsCount != 3 {
		t.Errorf("FindingsCount = %d, want 3", entry.FindingsCount)
	}
	if entry.UniqueFindings != 2 {
		t.Errorf("UniqueFindings = %d, want 2", entry.UniqueFindings)
	}
	if entry.Velocity != 2 {
		t.Errorf("Velocity = %.2f, want 2.00", entry.Velocity)
	}
}

func TestVelocityTracker_ZeroTokens(t *testing.T) {
	tracker := NewVelocityTracker()
	output := ModeOutput{
		ModeID: "formal",
		TopFindings: []Finding{
			{Finding: "Needs refactor", Impact: ImpactMedium, Confidence: 0.6},
		},
	}

	tracker.RecordOutput("formal", output, 0)
	report := tracker.CalculateVelocity()

	if len(tracker.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(tracker.Entries))
	}
	if tracker.Entries[0].Velocity != 0 {
		t.Errorf("Velocity = %.2f, want 0", tracker.Entries[0].Velocity)
	}
	if report.Overall != 0 {
		t.Errorf("Overall velocity = %.2f, want 0", report.Overall)
	}
}

func TestVelocityTracker_OverallAndLabels(t *testing.T) {
	tracker := NewVelocityTracker()
	outputA := ModeOutput{
		ModeID: "mode-a",
		TopFindings: []Finding{
			{Finding: "Finding A1", Impact: ImpactLow, Confidence: 0.5},
		},
	}
	outputB := ModeOutput{
		ModeID: "mode-b",
		TopFindings: []Finding{
			{Finding: "Finding B1", Impact: ImpactLow, Confidence: 0.5},
			{Finding: "Finding B2", Impact: ImpactLow, Confidence: 0.5},
		},
	}

	tracker.RecordOutput("mode-a", outputA, 2000)
	tracker.RecordOutput("mode-b", outputB, 1000)

	report := tracker.CalculateVelocity()
	expectedOverall := 1.0
	if math.Abs(report.Overall-expectedOverall) > 0.0001 {
		t.Errorf("Overall velocity = %.2f, want %.2f", report.Overall, expectedOverall)
	}
	if !containsStringValue(report.LowPerformers, "mode-a") {
		t.Errorf("expected mode-a to be low performer, got %v", report.LowPerformers)
	}
	if !containsStringValue(report.HighPerformers, "mode-b") {
		t.Errorf("expected mode-b to be high performer, got %v", report.HighPerformers)
	}
	if len(report.Suggestions) == 0 || !strings.Contains(report.Suggestions[0], "mode-a") {
		t.Errorf("expected suggestion for mode-a, got %v", report.Suggestions)
	}
}

func containsStringValue(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
