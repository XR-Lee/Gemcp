package researchbench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
)

// Tools is how an Agent reaches Gemcp: one MCP tool call, returning the
// structured result as a JSON object.
type Tools interface {
	Call(ctx context.Context, name string, arguments map[string]any) (map[string]any, error)
}

// Lab is everything around the Agent: the Owner who approves proposals and the
// backend that runs them. In a live deployment Execute is the scheduler; in
// the tests it is the task simulator.
type Lab interface {
	Approve(ctx context.Context, task Task, proposal map[string]any) error
	Execute(ctx context.Context, experimentID string) error
}

// ErrBudget is what a Lab returns when the Owner refuses a proposal because
// the task's run budget is spent.
var ErrBudget = errors.New("run budget exhausted")

// Run is one Experiment as the Agent saw it.
type Run struct {
	Params       map[string]int
	ExperimentID string
	ResultID     string
	Crashed      bool
	Metric       float64
	HasMetric    bool
	// Verdict is the Agent's reading of the run against the task criterion.
	Verdict string
}

// Conclusion is the Agent's study-level answer.
type Conclusion struct {
	Verdict    string
	Best       map[string]int
	BestMetric float64
	HasBest    bool
	Mean       float64
	Replicates int
}

// Policy decides what an Agent runs next and what the evidence means. The
// driver in Play does everything else the same way for every policy.
type Policy interface {
	Name() string
	Next(task Task, runs []Run) (map[string]int, bool)
	Conclude(task Task, runs []Run) Conclusion
}

// Episode records one Agent working one task.
type Episode struct {
	Agent        string
	Task         string
	StudyID      string
	HypothesisID string
	Runs         []Run
	Conclusion   Conclusion
	Report       string
	// LegacyVerdicts counts runs where close_run did not accept a verdict and
	// the Agent recorded its judgment as a separate observation instead.
	LegacyVerdicts int
	// CrashNextActions are the decision prompts get_next_actions showed right
	// after a crashed run was closed.
	CrashNextActions []string
	// ToolCalls and ResponseBytes are what the Agent spent: calls made and
	// JSON bytes of structured results read.
	ToolCalls     int
	ResponseBytes int
}

// meteredTools counts what an Agent spends talking to Gemcp.
type meteredTools struct {
	inner Tools
	calls int
	bytes int
}

func (m *meteredTools) Call(ctx context.Context, name string, arguments map[string]any) (map[string]any, error) {
	result, err := m.inner.Call(ctx, name, arguments)
	m.calls++
	if result != nil {
		if encoded, marshalErr := json.Marshal(result); marshalErr == nil {
			m.bytes += len(encoded)
		}
	}
	return result, err
}

// Play drives one policy through Gemcp on one task the way an MCP Agent
// would: open a Study, record the hypothesis and an outside claim, run
// prepared Experiments the Owner approves, close each run with a verdict,
// record a decision, and export the plan-sync report.
func Play(ctx context.Context, tools Tools, lab Lab, policy Policy, task Task) (Episode, error) {
	metered := &meteredTools{inner: tools}
	episode := Episode{Agent: policy.Name(), Task: task.ID}
	err := play(ctx, metered, lab, policy, task, &episode)
	episode.ToolCalls, episode.ResponseBytes = metered.calls, metered.bytes
	return episode, err
}

func play(ctx context.Context, tools Tools, lab Lab, policy Policy, task Task, episode *Episode) error {
	if err := openStudy(ctx, tools, task, policy.Name()+"-"+task.ID, episode); err != nil {
		return err
	}
	if err := recordPrior(ctx, tools, task, episode); err != nil {
		return err
	}
	for len(episode.Runs) < task.Budget {
		params, more := policy.Next(task, episode.Runs)
		if !more {
			break
		}
		run, err := runOnce(ctx, tools, lab, task, episode, params)
		if errors.Is(err, ErrBudget) {
			break
		}
		if err != nil {
			return err
		}
		episode.Runs = append(episode.Runs, run)
	}
	episode.Conclusion = policy.Conclude(task, episode.Runs)
	from := episode.HypothesisID
	if n := len(episode.Runs); n > 0 && episode.Runs[n-1].ResultID != "" {
		from = episode.Runs[n-1].ResultID
	}
	if _, err := tools.Call(ctx, "update_research_workspace", graphNode(episode.StudyID, map[string]any{
		"kind": "decision", "title": "Decision: hypothesis " + episode.Conclusion.Verdict,
		"summary": conclusionSummary(task, episode.Conclusion, episode.Runs), "from_node_id": from, "relation": "leads_to",
	})); err != nil {
		return fmt.Errorf("record decision: %w", err)
	}
	report, err := tools.Call(ctx, "export_research_plan_sync", map[string]any{"study_id": episode.StudyID})
	if err != nil {
		return fmt.Errorf("export report: %w", err)
	}
	episode.Report = text(report, "markdown")
	return nil
}

// recordPrior notes an unverified outside claim and attaches it the way most
// Agents do: an update by id that carries only the new edge.
func recordPrior(ctx context.Context, tools Tools, task Task, episode *Episode) error {
	title := "Prior report: " + task.Prior
	prior, err := tools.Call(ctx, "update_research_workspace", graphNode(episode.StudyID, map[string]any{
		"kind": "observation", "title": title, "summary": "Unverified claim from outside the lab.",
		"metric_name": task.Metric, "metric_value": task.PriorValue,
		"from_node_id": episode.HypothesisID, "relation": "leads_to",
	}))
	if err != nil {
		return fmt.Errorf("record prior: %w", err)
	}
	if _, err := tools.Call(ctx, "update_research_workspace", graphNode(episode.StudyID, map[string]any{
		"id": episode.HypothesisID, "kind": "hypothesis", "title": task.Hypothesis,
		"from_node_id": nodeID(prior, "observation", title), "relation": "supports",
	})); err != nil {
		return fmt.Errorf("attach prior: %w", err)
	}
	return nil
}

func runOnce(ctx context.Context, tools Tools, lab Lab, task Task, episode *Episode, params map[string]int) (Run, error) {
	run := Run{Params: task.withDefaults(params)}
	label := task.DescribeParams(params)
	prepared, err := tools.Call(ctx, "prepare_experiment", map[string]any{
		"workload": task.ID, "parameters": task.WorkloadParameters(params),
		"from_node_id": episode.HypothesisID, "expected_metric": task.Metric,
	})
	if err != nil {
		return run, fmt.Errorf("prepare %s: %w", label, err)
	}
	proposal, _ := field(prepared, "proposal").(map[string]any)
	if proposal == nil {
		return run, fmt.Errorf("prepare %s returned no proposal", label)
	}
	if err := lab.Approve(ctx, task, proposal); err != nil {
		return run, err
	}
	submitted, err := tools.Call(ctx, "submit_prepared_experiment", map[string]any{
		"proposal_id": text(proposal, "id"), "confirmation_digest": text(proposal, "confirmation_digest"),
	})
	if err != nil {
		return run, fmt.Errorf("submit %s: %w", label, err)
	}
	run.ExperimentID = text(submitted, "experiment", "id")
	if warning := text(submitted, "graph_bind_warning"); warning != "" {
		return run, fmt.Errorf("submit %s spent without a Graph run: %s", label, warning)
	}
	if err := lab.Execute(ctx, run.ExperimentID); err != nil {
		return run, fmt.Errorf("execute %s: %w", label, err)
	}
	view, err := tools.Call(ctx, "get_experiment", map[string]any{"experiment_id": run.ExperimentID})
	if err != nil {
		return run, fmt.Errorf("monitor %s: %w", label, err)
	}
	run.Crashed = text(view, "state") != "succeeded"
	run.Metric, run.HasMetric = number(view, "metrics", task.Metric)
	outcome := Outcome{Crashed: run.Crashed}
	if run.HasMetric {
		outcome.Metrics = map[string]float64{task.Metric: run.Metric}
	}
	run.Verdict = task.ExpectedVerdict(outcome)
	closed, err := closeRun(ctx, tools, task, episode, run, task.ID+" "+label)
	if err != nil {
		return run, fmt.Errorf("close %s: %w", label, err)
	}
	run.ResultID = resultOf(closed, run.ExperimentID)
	if run.Crashed {
		actions, err := tools.Call(ctx, "get_next_actions", map[string]any{"study_id": episode.StudyID})
		if err != nil {
			return run, fmt.Errorf("next actions after %s: %w", label, err)
		}
		for _, action := range objects(actions, "actions") {
			if text(action, "kind") == "record_decision" {
				episode.CrashNextActions = append(episode.CrashNextActions, text(action, "detail"))
			}
		}
	}
	return run, nil
}

// closeRun records the result with the Agent's verdict. A server from before
// the verdict field rejects it as an unknown argument; the Agent then closes
// without it and records its judgment as an observation on the result, which
// is the best an Agent can do there.
func closeRun(ctx context.Context, tools Tools, task Task, episode *Episode, run Run, title string) (map[string]any, error) {
	arguments := map[string]any{
		"study_id": episode.StudyID, "experiment_id": run.ExperimentID,
		"title": title, "summary": runSummary(task, run), "verdict": run.Verdict,
	}
	closed, err := tools.Call(ctx, "close_run", arguments)
	if err == nil || !strings.Contains(err.Error(), "verdict") {
		return closed, err
	}
	episode.LegacyVerdicts++
	delete(arguments, "verdict")
	closed, err = tools.Call(ctx, "close_run", arguments)
	if err != nil || run.Verdict == VerdictInconclusive {
		return closed, err
	}
	noteTitle := "Verdict on " + title + ": " + run.Verdict
	noted, err := tools.Call(ctx, "update_research_workspace", graphNode(episode.StudyID, map[string]any{
		"kind": "observation", "title": noteTitle, "summary": runSummary(task, run),
		"from_node_id": resultOf(closed, run.ExperimentID), "relation": "leads_to",
	}))
	if err != nil {
		return nil, err
	}
	if _, err := tools.Call(ctx, "update_research_workspace", graphNode(episode.StudyID, map[string]any{
		"id": episode.HypothesisID, "kind": "hypothesis", "title": task.Hypothesis,
		"from_node_id": nodeID(noted, "observation", noteTitle), "relation": run.Verdict,
	})); err != nil {
		return nil, err
	}
	return noted, nil
}

func runSummary(task Task, run Run) string {
	if run.Crashed {
		return "The run crashed before reporting " + task.Metric + "."
	}
	return fmt.Sprintf("%s %s at %s.", task.Metric, formatMetric(run.Metric), task.DescribeParams(run.Params))
}

func conclusionSummary(task Task, conclusion Conclusion, runs []Run) string {
	counts := map[string]int{}
	for _, run := range runs {
		counts[run.Verdict]++
	}
	summary := "Conclusion: " + conclusion.Verdict + "."
	if conclusion.HasBest {
		summary += fmt.Sprintf(" Best %s=%d %s=%s.", task.Knob, conclusion.Best[task.Knob], task.Metric, formatMetric(conclusion.BestMetric))
	}
	if conclusion.Replicates > 0 {
		summary += fmt.Sprintf(" Mean %s=%s over %d seeds.", task.Metric, formatMetric(conclusion.Mean), conclusion.Replicates)
	}
	return summary + fmt.Sprintf(" %d runs: %d supports, %d contradicts, %d inconclusive.",
		len(runs), counts[VerdictSupports], counts[VerdictContradicts], counts[VerdictInconclusive])
}

func graphNode(studyID string, node map[string]any) map[string]any {
	node["study_id"] = studyID
	return map[string]any{"node": node}
}

func nodeID(workspace map[string]any, kind, title string) string {
	for _, node := range objects(workspace, "study", "nodes") {
		if text(node, "kind") == kind && (title == "" || text(node, "title") == title) {
			return text(node, "id")
		}
	}
	return ""
}

func resultOf(workspace map[string]any, experimentID string) string {
	runID := ""
	for _, node := range objects(workspace, "study", "nodes") {
		if text(node, "kind") == "run" && text(node, "experiment_id") == experimentID {
			runID = text(node, "id")
		}
	}
	for _, edge := range objects(workspace, "study", "edges") {
		if runID != "" && text(edge, "from_id") == runID && text(edge, "relation") == "produced" {
			return text(edge, "to_id")
		}
	}
	return ""
}

// Careful explores a search range coarse to fine and replicates an effect
// over several seeds before concluding. It is the reference for a good Agent.
type Careful struct{}

func (Careful) Name() string { return "careful" }

func (Careful) Next(task Task, runs []Run) (map[string]int, bool) {
	if task.Style == Replicate {
		return replicateNext(task, runs, MinReplicates)
	}
	return coarseToFine(task, runs)
}

func (Careful) Conclude(task Task, runs []Run) Conclusion { return evidenceConclusion(task, runs) }

// Grid spends the whole budget on an evenly spaced grid, or on every seed it
// can afford. It finds a narrow optimum only by luck.
type Grid struct{}

func (Grid) Name() string { return "grid" }

func (Grid) Next(task Task, runs []Run) (map[string]int, bool) {
	if task.Style == Replicate {
		return replicateNext(task, runs, task.Budget)
	}
	tried := triedValues(task, runs)
	for _, point := range gridPoints(task.KnobMin, task.KnobMax, task.Budget) {
		if !tried[point] {
			return map[string]int{task.Knob: point}, true
		}
	}
	return nil, false
}

func (Grid) Conclude(task Task, runs []Run) Conclusion { return evidenceConclusion(task, runs) }

// Hasty runs one Experiment and concludes from it, reading a crash as a
// negative result.
type Hasty struct{}

func (Hasty) Name() string { return "hasty" }

func (Hasty) Next(task Task, runs []Run) (map[string]int, bool) {
	if len(runs) > 0 {
		return nil, false
	}
	if task.Style == Replicate {
		return withSeed(task.Treatment, 1), true
	}
	return map[string]int{task.Knob: task.KnobMax}, true
}

func (Hasty) Conclude(task Task, runs []Run) Conclusion {
	conclusion := Conclusion{Verdict: ConclusionInconclusive}
	if len(runs) == 0 {
		return conclusion
	}
	run := runs[0]
	switch {
	case run.Crashed, run.Verdict == VerdictContradicts:
		conclusion.Verdict = ConclusionRefuted
	case run.Verdict == VerdictSupports:
		conclusion.Verdict = ConclusionSupported
	}
	if task.Style == Search && run.HasMetric {
		conclusion.Best, conclusion.BestMetric, conclusion.HasBest = run.Params, run.Metric, true
	}
	return conclusion
}

// evidenceConclusion applies the task criterion to all the evidence: one
// supporting run proves an existence claim; an effect claim needs at least
// MinReplicates seeds and is judged on their mean.
func evidenceConclusion(task Task, runs []Run) Conclusion {
	conclusion := Conclusion{Verdict: ConclusionInconclusive}
	var measuredRuns []Run
	for _, run := range runs {
		if !run.Crashed && run.HasMetric {
			measuredRuns = append(measuredRuns, run)
		}
	}
	if task.Style == Search {
		for _, run := range measuredRuns {
			if !conclusion.HasBest || task.Better(run.Metric, conclusion.BestMetric) {
				conclusion.Best, conclusion.BestMetric, conclusion.HasBest = run.Params, run.Metric, true
			}
		}
	}
	switch task.Claim {
	case ExistenceClaim:
		for _, run := range measuredRuns {
			if run.Verdict == VerdictSupports {
				conclusion.Verdict = ConclusionSupported
			}
		}
	case EffectClaim:
		if len(measuredRuns) < MinReplicates {
			return conclusion
		}
		sum := 0.0
		for _, run := range measuredRuns {
			sum += run.Metric
		}
		conclusion.Mean, conclusion.Replicates = sum/float64(len(measuredRuns)), len(measuredRuns)
		conclusion.Verdict = ConclusionRefuted
		if task.reaches(conclusion.Mean, task.Threshold) {
			conclusion.Verdict = ConclusionSupported
		}
	}
	return conclusion
}

// coarseToFine probes evenly spaced points with up to half the budget, then
// repeatedly splits the wider gap next to the best point so far, leaning
// toward its better neighbour.
func coarseToFine(task Task, runs []Run) (map[string]int, bool) {
	tried := triedValues(task, runs)
	for _, point := range gridPoints(task.KnobMin, task.KnobMax, min(4, max(2, task.Budget/2))) {
		if !tried[point] {
			return map[string]int{task.Knob: point}, true
		}
	}
	score := map[int]float64{}
	var points []int
	for _, run := range runs {
		value := run.Params[task.Knob]
		points = append(points, value)
		if !run.Crashed && run.HasMetric {
			score[value] = run.Metric
		}
	}
	sort.Ints(points)
	ranked := make([]int, 0, len(score))
	for value := range score {
		ranked = append(ranked, value)
	}
	sort.Slice(ranked, func(i, j int) bool {
		if score[ranked[i]] != score[ranked[j]] {
			return task.Better(score[ranked[i]], score[ranked[j]])
		}
		return ranked[i] < ranked[j]
	})
	better := func(a, b int) bool {
		scoreA, okA := score[a]
		scoreB, okB := score[b]
		return okA && (!okB || task.Better(scoreA, scoreB))
	}
	for _, best := range ranked {
		index := sort.SearchInts(points, best)
		type side struct{ gap, mid, neighbour int }
		var sides []side
		if index > 0 && best-points[index-1] >= 2 {
			left := points[index-1]
			sides = append(sides, side{gap: best - left, mid: left + (best-left)/2, neighbour: left})
		}
		if index+1 < len(points) && points[index+1]-best >= 2 {
			right := points[index+1]
			sides = append(sides, side{gap: right - best, mid: best + (right-best)/2, neighbour: right})
		}
		sort.SliceStable(sides, func(i, j int) bool {
			if sides[i].gap != sides[j].gap {
				return sides[i].gap > sides[j].gap
			}
			return better(sides[i].neighbour, sides[j].neighbour)
		})
		for _, candidate := range sides {
			if !tried[candidate.mid] {
				return map[string]int{task.Knob: candidate.mid}, true
			}
		}
	}
	return nil, false
}

func replicateNext(task Task, runs []Run, limit int) (map[string]int, bool) {
	if len(runs) >= limit {
		return nil, false
	}
	used := map[int]bool{}
	for _, run := range runs {
		used[run.Params["seed"]] = true
	}
	for seed := seedParameter.Min; seed <= seedParameter.Max; seed++ {
		if !used[seed] {
			return withSeed(task.Treatment, seed), true
		}
	}
	return nil, false
}

func withSeed(treatment map[string]int, seed int) map[string]int {
	params := map[string]int{"seed": seed}
	for name, value := range treatment {
		params[name] = value
	}
	return params
}

func triedValues(task Task, runs []Run) map[int]bool {
	tried := map[int]bool{}
	for _, run := range runs {
		tried[run.Params[task.Knob]] = true
	}
	return tried
}

// gridPoints spreads n integer points evenly over [lo, hi].
func gridPoints(lo, hi, n int) []int {
	if n <= 1 || hi <= lo {
		return []int{lo}
	}
	var points []int
	seen := map[int]bool{}
	for i := 0; i < n; i++ {
		point := lo + int(math.Round(float64(i*(hi-lo))/float64(n-1)))
		if !seen[point] {
			seen[point] = true
			points = append(points, point)
		}
	}
	return points
}
