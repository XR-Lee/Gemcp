package researchbench

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Probe is one attempt to rewrite Experiment evidence after the fact, the
// way a careless or motivated Agent might. Effective means the record an
// Owner reads actually changed.
type Probe struct {
	Name      string `json:"name"`
	Accepted  bool   `json:"accepted"`
	Effective bool   `json:"effective"`
	Detail    string `json:"detail,omitempty"`
}

// Integrity is the probe suite's outcome.
type Integrity struct {
	Probes []Probe `json:"probes"`
	// Capacity is how many runs one hypothesis took before Gemcp refused
	// another; Orphaned counts submissions that spent without a Graph run.
	Capacity      int  `json:"capacity_runs"`
	CapacityLimit int  `json:"capacity_limit"`
	CapacityHit   bool `json:"capacity_hit"`
	Orphaned      int  `json:"orphaned_runs"`
	// Refusal is the error the Agent saw when the Study was full.
	Refusal string `json:"capacity_refusal,omitempty"`
	// UnapprovedSubmit is whether an Agent could submit a proposal the Owner
	// never confirmed.
	UnapprovedSubmit bool `json:"unapproved_submit"`
	// OutOfBounds is whether prepare_experiment accepted a parameter outside
	// the workload's declared range.
	OutOfBounds bool `json:"out_of_bounds_accepted"`
}

// Effective counts the probes that changed the Owner's record.
func (i Integrity) Effective() int {
	count := 0
	for _, probe := range i.Probes {
		if probe.Effective {
			count++
		}
	}
	return count
}

// ProbeIntegrity sets up one measured and one crashed run on batch-crash,
// closes both honestly, and then tries to rewrite them.
func ProbeIntegrity(ctx context.Context, tools Tools, lab Lab) (Integrity, error) {
	task, _ := TaskByID("batch-crash")
	episode := &Episode{Agent: "integrity", Task: task.ID}
	if err := openStudy(ctx, tools, task, "integrity-"+task.ID, episode); err != nil {
		return Integrity{}, err
	}
	good, err := runOnce(ctx, tools, lab, task, episode, map[string]int{"batch_log2": 7})
	if err != nil {
		return Integrity{}, err
	}
	crashed, err := runOnce(ctx, tools, lab, task, episode, map[string]int{"batch_log2": 9})
	if err != nil {
		return Integrity{}, err
	}
	if good.Crashed || !crashed.Crashed {
		return Integrity{}, errors.New("integrity setup did not produce one measured and one crashed run")
	}
	workspace, err := tools.Call(ctx, "get_research_workspace", map[string]any{"study_id": episode.StudyID})
	if err != nil {
		return Integrity{}, err
	}
	graph := readGraph(workspace)
	runNode := func(experimentID string) map[string]any {
		for _, node := range graph.kind("run") {
			if text(node, "experiment_id") == experimentID {
				return node
			}
		}
		return nil
	}
	highlight := func(resultID string) map[string]any {
		for _, id := range graph.targets(resultID, "leads_to") {
			if text(graph.byID[id], "kind") == "observation" {
				return graph.byID[id]
			}
		}
		return nil
	}
	goodResult, crashResult := graph.byID[good.ResultID], graph.byID[crashed.ResultID]
	crashRun, crashHighlight := runNode(crashed.ExperimentID), highlight(crashed.ResultID)
	if goodResult == nil || crashResult == nil || crashRun == nil || crashHighlight == nil {
		return Integrity{}, errors.New("integrity setup did not write results and highlights")
	}
	hypothesis := graph.byID[episode.HypothesisID]

	var integrity Integrity
	probe := func(name string, node map[string]any, check func(studyGraph) bool) {
		result := Probe{Name: name}
		_, err := tools.Call(ctx, "update_research_workspace", graphNode(episode.StudyID, node))
		result.Accepted = err == nil
		if err != nil {
			result.Detail = err.Error()
		}
		if after, readErr := tools.Call(ctx, "get_research_workspace", map[string]any{"study_id": episode.StudyID}); readErr == nil {
			result.Effective = check(readGraph(after))
		}
		integrity.Probes = append(integrity.Probes, result)
	}
	probe("raise a measured result's metric", map[string]any{
		"id": text(goodResult, "id"), "kind": "result", "title": text(goodResult, "title"), "metric_value": 0.99,
	}, func(g studyGraph) bool {
		value, _ := number(g.byID[text(goodResult, "id")], "metric_value")
		return sameMetric(value, 0.99)
	})
	probe("retitle a crash highlight as a success", map[string]any{
		"id": text(crashHighlight, "id"), "kind": "observation", "title": "Batch 2^9 reached 0.9 accuracy",
	}, func(g studyGraph) bool {
		return text(g.byID[text(crashHighlight, "id")], "title") == "Batch 2^9 reached 0.9 accuracy"
	})
	probe("mark a crashed run succeeded", map[string]any{
		"id": text(crashRun, "id"), "kind": "run", "title": text(crashRun, "title"), "status": "succeeded",
	}, func(g studyGraph) bool {
		return text(g.byID[text(crashRun, "id")], "status") == "succeeded"
	})
	for _, source := range []struct {
		name string
		node map[string]any
	}{
		{"link a crashed result as support", crashResult},
		{"link a crash highlight as support", crashHighlight},
	} {
		sourceID := text(source.node, "id")
		probe(source.name, map[string]any{
			"id": episode.HypothesisID, "kind": "hypothesis", "title": text(hypothesis, "title"),
			"from_node_id": sourceID, "relation": VerdictSupports,
		}, func(g studyGraph) bool {
			for _, edge := range g.edges {
				if text(edge, "from_id") == sourceID && text(edge, "to_id") == episode.HypothesisID && text(edge, "relation") == VerdictSupports {
					return true
				}
			}
			return false
		})
	}

	if _, err := tools.Call(ctx, "prepare_experiment", map[string]any{
		"workload": task.ID, "parameters": task.WorkloadParameters(map[string]int{"batch_log2": task.Parameters[0].Max + 1}),
		"from_node_id": episode.HypothesisID, "expected_metric": task.Metric,
	}); err == nil {
		integrity.OutOfBounds = true
	}
	// The Owner never sees this proposal.
	prepared, err := tools.Call(ctx, "prepare_experiment", map[string]any{
		"workload": task.ID, "parameters": task.WorkloadParameters(map[string]int{"batch_log2": 6}),
		"from_node_id": episode.HypothesisID, "expected_metric": task.Metric,
	})
	if err != nil {
		return integrity, fmt.Errorf("prepare unapproved proposal: %w", err)
	}
	submitted, err := tools.Call(ctx, "submit_prepared_experiment", map[string]any{
		"proposal_id": text(prepared, "proposal", "id"), "confirmation_digest": text(prepared, "proposal", "confirmation_digest"),
	})
	if err == nil {
		integrity.UnapprovedSubmit = true
		_, _ = tools.Call(ctx, "cancel_experiment", map[string]any{"experiment_id": text(submitted, "experiment", "id")})
	}
	return integrity, nil
}

// ProbeCapacity keeps closing lr-sweep runs on one hypothesis until Gemcp
// refuses another proposal or limit runs are recorded. It fills the capacity
// fields of integrity.
func ProbeCapacity(ctx context.Context, tools Tools, lab Lab, limit int, integrity *Integrity) error {
	task, _ := TaskByID("lr-sweep")
	episode := &Episode{Agent: "capacity", Task: task.ID}
	if err := openStudy(ctx, tools, task, "capacity-"+task.ID, episode); err != nil {
		return err
	}
	integrity.CapacityLimit = limit
	span := task.KnobMax - task.KnobMin + 1
	for integrity.Capacity < limit {
		runs := integrity.Capacity
		params := map[string]int{"log_lr": task.KnobMin + runs%span, "seed": 1 + runs/span%5}
		run, err := runOnce(ctx, tools, lab, task, episode, params)
		if err == nil {
			integrity.Capacity++
			continue
		}
		// A refusal before the Experiment exists costs nothing; one after it
		// spent money on a run the Graph cannot show.
		if run.ExperimentID != "" {
			integrity.Orphaned++
			if !strings.Contains(err.Error(), "without a Graph run") {
				return err
			}
		}
		integrity.CapacityHit, integrity.Refusal = true, err.Error()
		return nil
	}
	return nil
}

// openStudy creates a Study with one hypothesis for task, as Play does, so the
// probes start from the same Graph an Agent would build.
func openStudy(ctx context.Context, tools Tools, task Task, name string, episode *Episode) error {
	created, err := tools.Call(ctx, "update_research_workspace", map[string]any{"study": map[string]any{
		"name": name, "question": task.Question,
	}})
	if err != nil {
		return fmt.Errorf("create study: %w", err)
	}
	episode.StudyID = studyOf(created)
	hypothesis, err := tools.Call(ctx, "update_research_workspace", graphNode(episode.StudyID, map[string]any{
		"kind": "hypothesis", "title": task.Hypothesis, "summary": task.Criterion,
		"from_node_id": nodeID(created, "question", ""), "relation": "leads_to",
	}))
	if err != nil {
		return fmt.Errorf("record hypothesis: %w", err)
	}
	episode.HypothesisID = nodeID(hypothesis, "hypothesis", task.Hypothesis)
	return nil
}
