package researchbench

import "testing"

func TestAgentObservationDoesNotConflictTheCloseRunVerdict(t *testing.T) {
	graph := studyGraph{
		byID: map[string]map[string]any{
			"result": {"id": "result", "kind": "result"},
			"highlight": {
				"id": "highlight", "kind": "observation", "system_written": true, "title": "batch 2^7 seed 1",
			},
			"reading": {
				"id": "reading", "kind": "observation", "title": "Agent reading: 0.88 looks like seed noise",
			},
		},
		edges: []map[string]any{
			{"from_id": "result", "to_id": "highlight", "relation": "leads_to"},
			{"from_id": "highlight", "to_id": "hypothesis", "relation": "supports"},
			{"from_id": "result", "to_id": "reading", "relation": "leads_to"},
			{"from_id": "reading", "to_id": "hypothesis", "relation": "contradicts"},
		},
	}
	evidence := runVerdictEvidence(graph, "result")
	if recorded := recordedVerdict(graph, evidence, "hypothesis"); recorded != VerdictSupports {
		t.Fatalf("recorded verdict = %q", recorded)
	}
	if got := agentReading(graph, "result", "hypothesis"); got != VerdictContradicts {
		t.Fatalf("agent reading = %q", got)
	}
	record := map[string]any{"state": "succeeded", "metric_value": 0.88, "verdict": "supports", "agent_reading": "contradicts"}
	metric := 0.88
	if !ownerRecordComplete(record, RunScore{State: "succeeded", Metric: &metric, Recorded: VerdictSupports, AgentReading: VerdictContradicts}) {
		t.Fatal("owner card hid the agent reading")
	}
	hidden := map[string]any{"state": "succeeded", "metric_value": 0.88, "verdict": "supports"}
	if ownerRecordComplete(hidden, RunScore{State: "succeeded", Metric: &metric, Recorded: VerdictSupports, AgentReading: VerdictContradicts}) {
		t.Fatal("owner card without the agent reading was treated as complete")
	}
	line := "batch 2^7 seed 1, val_accuracy = 0.88 (measured). Verdict: supports. Agent reading: contradicts. (Agent reading: 0.88 looks like seed noise)"
	got, ok := lineVerdict(line)
	if !ok || got != VerdictSupports {
		t.Fatalf("line verdict = %q ok=%t", got, ok)
	}
}
