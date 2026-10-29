package researchbench

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
)

// TargetScore is the normalized score at which a search task counts as
// solved: the Agent found at least 95% of the reachable gain.
const TargetScore = 0.95

// Score grades what Gemcp holds about one Study after an Agent worked a task.
// Everything except the Agent-side counters comes back through MCP, so the
// grade reflects the record an Owner would read, not the Agent's own account.
type Score struct {
	Agent   string `json:"agent"`
	Task    string `json:"task"`
	StudyID string `json:"study_id"`

	// Exploration.
	Runs       int      `json:"runs"`
	Budget     int      `json:"budget"`
	Crashes    int      `json:"crashes"`
	Duplicates int      `json:"duplicate_runs"`
	BestMetric *float64 `json:"best_metric,omitempty"`
	// Normalized is (best - baseline) / (oracle - baseline) on search tasks.
	Normalized   *float64 `json:"normalized_score,omitempty"`
	RunsToTarget int      `json:"runs_to_target,omitempty"`

	// Conclusion read from the Agent's last decision node.
	Conclusion string `json:"conclusion"`
	Truth      string `json:"truth"`

	// Graph record.
	Closed            int  `json:"closed_runs"`
	LabMismatches     int  `json:"lab_mismatches"`
	MetricMismatches  int  `json:"metric_mismatches"`
	VerdictCorrect    int  `json:"verdict_correct"`
	FalseSupports     int  `json:"false_supports"`
	CrashAsEvidence   int  `json:"crash_as_evidence"`
	Conflicts         int  `json:"conflicting_verdicts"`
	CriterionKept     bool `json:"criterion_kept"`
	MetricNodes       int  `json:"metric_nodes"`
	ProvenanceCorrect int  `json:"provenance_correct"`
	OwnerRecords      int  `json:"owner_records_complete"`

	// Plan-sync report.
	ReportChecks  int      `json:"report_checks"`
	ReportPassed  int      `json:"report_passed"`
	ReportMissing []string `json:"report_missing,omitempty"`

	// Agent-side counters, copied from the Episode.
	CrashPrompts      int `json:"crash_prompts"`
	MisleadingPrompts int `json:"misleading_prompts"`
	LegacyVerdicts    int `json:"legacy_verdicts"`
	ToolCalls         int `json:"tool_calls"`
	ResponseBytes     int `json:"response_bytes"`

	RunScores []RunScore `json:"run_scores"`
	Findings  []string   `json:"findings,omitempty"`
}

// RunScore grades one Experiment of the Study.
type RunScore struct {
	ExperimentID string   `json:"experiment_id"`
	Params       string   `json:"params"`
	State        string   `json:"state"`
	Metric       *float64 `json:"metric,omitempty"`
	// Expected is the verdict the task criterion gives the measured outcome.
	// Recorded is the close_run highlight's verdict, or "conflict" when that
	// evidence itself says both supports and contradicts. An Agent observation
	// linked from the result is AgentReading, not a second verdict.
	Expected     string `json:"expected_verdict"`
	Recorded     string `json:"recorded_verdict"`
	AgentReading string `json:"agent_reading,omitempty"`
	Closed       bool   `json:"closed"`
}

// ConclusionCorrect reports whether the Agent's decision matches the truth.
func (s Score) ConclusionCorrect() bool { return s.Conclusion == s.Truth }

// AddEpisode copies what only the Agent could observe.
func (s *Score) AddEpisode(episode Episode) {
	s.Agent = episode.Agent
	s.CrashPrompts = len(episode.CrashNextActions)
	s.MisleadingPrompts = 0
	for _, detail := range episode.CrashNextActions {
		if misleadingCrashPrompt(detail) {
			s.MisleadingPrompts++
		}
	}
	s.LegacyVerdicts = episode.LegacyVerdicts
	s.ToolCalls, s.ResponseBytes = episode.ToolCalls, episode.ResponseBytes
}

// misleadingCrashPrompt reports whether the next step Gemcp suggested after a
// crashed run fails to say that the run is no evidence. Quoted node titles
// are ignored: a task named batch-crash says nothing about the run.
func misleadingCrashPrompt(detail string) bool {
	return !mentionsFailure(quoted.ReplaceAllString(detail, ""))
}

var quoted = regexp.MustCompile(`"[^"]*"`)

func mentionsFailure(text string) bool {
	lower := strings.ToLower(text)
	for _, marker := range []string{"fail", "crash", "valid evidence", "no evidence", "not evidence"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

type studyGraph struct {
	nodes []map[string]any
	byID  map[string]map[string]any
	edges []map[string]any
}

func readGraph(workspace map[string]any) studyGraph {
	graph := studyGraph{nodes: objects(workspace, "study", "nodes"), byID: map[string]map[string]any{}, edges: objects(workspace, "study", "edges")}
	for _, node := range graph.nodes {
		graph.byID[text(node, "id")] = node
	}
	return graph
}

func (g studyGraph) targets(fromID, relation string) []string {
	var ids []string
	for _, edge := range g.edges {
		if text(edge, "from_id") == fromID && text(edge, "relation") == relation {
			ids = append(ids, text(edge, "to_id"))
		}
	}
	return ids
}

func (g studyGraph) kind(kind string) []map[string]any {
	var found []map[string]any
	for _, node := range g.nodes {
		if text(node, "kind") == kind {
			found = append(found, node)
		}
	}
	return found
}

type scoredRun struct {
	node       map[string]any
	experiment map[string]any
	created    time.Time
	params     map[string]int
	outcome    Outcome
}

// ScoreStudy grades a Study against task.
func ScoreStudy(ctx context.Context, tools Tools, task Task, studyID string) (Score, error) {
	score := Score{Task: task.ID, StudyID: studyID, Budget: task.Budget, Truth: task.Truth}
	workspace, err := tools.Call(ctx, "get_research_workspace", map[string]any{"study_id": studyID})
	if err != nil {
		return score, err
	}
	graph := readGraph(workspace)
	hypotheses := graph.kind("hypothesis")
	if len(hypotheses) == 0 {
		return score, fmt.Errorf("study %s has no hypothesis", studyID)
	}
	hypothesis := hypotheses[0]
	hypothesisID := text(hypothesis, "id")
	score.CriterionKept = oneLine(text(hypothesis, "summary")) == oneLine(task.Criterion)
	if !score.CriterionKept {
		score.finding("hypothesis summary %q no longer states the criterion", text(hypothesis, "summary"))
	}

	runs, err := loadRuns(ctx, tools, task, graph)
	if err != nil {
		return score, err
	}
	ownerRecords := map[string]map[string]any{}
	for _, record := range ownerRunRecords(workspace, hypothesisID) {
		ownerRecords[text(record, "run_node_id")] = record
	}
	report, err := tools.Call(ctx, "export_research_plan_sync", map[string]any{"study_id": studyID})
	if err != nil {
		return score, err
	}
	reportLines := strings.Split(text(report, "markdown"), "\n")
	score.reportCheck(containsLine(reportLines, task.Hypothesis), "hypothesis title")
	score.reportCheck(containsLine(reportLines, task.Criterion), "hypothesis criterion")

	seen := map[string]bool{}
	measuredBy := map[string]map[string]any{}
	oracle, _, hasOracle := task.Oracle()
	for index, run := range runs {
		label := task.DescribeParams(run.params)
		runID := text(run.node, "id")
		measuredBy[runID] = experimentMetrics(run.experiment)
		value, measured := run.outcome.Metrics[task.Metric]
		scored := RunScore{
			ExperimentID: text(run.experiment, "id"), Params: label, State: text(run.experiment, "state"),
			Expected: task.ExpectedVerdict(run.outcome),
		}
		if measured {
			scored.Metric = &value
		}
		score.Runs++
		if seen[label] {
			score.Duplicates++
		}
		seen[label] = true
		if run.outcome.Crashed {
			score.Crashes++
		}
		if simulated := task.Simulate(run.params); !sameOutcome(task, simulated, run.outcome) {
			score.LabMismatches++
			score.finding("%s: Experiment record does not match what the workload computes", label)
		}
		if measured {
			if score.BestMetric == nil || task.Better(value, *score.BestMetric) {
				best := value
				score.BestMetric = &best
			}
			if hasOracle && score.RunsToTarget == 0 && normalized(task, value, oracle) >= TargetScore {
				score.RunsToTarget = index + 1
			}
		}

		resultIDs := graph.targets(runID, "produced")
		scored.Closed = len(resultIDs) > 0
		evidence := map[string]bool{}
		if scored.Closed {
			score.Closed++
			result := graph.byID[resultIDs[0]]
			evidence = runVerdictEvidence(graph, resultIDs[0])
			scored.AgentReading = agentReading(graph, resultIDs[0], hypothesisID)
			resultValue, resultHasValue := number(result, "metric_value")
			switch {
			case !measured && resultHasValue:
				score.MetricMismatches++
				score.finding("%s: crashed run's result carries %s=%s", label, text(result, "metric_name"), formatMetric(resultValue))
			case measured && (!resultHasValue || text(result, "metric_name") != task.Metric || !sameMetric(resultValue, value)):
				score.MetricMismatches++
				score.finding("%s: result records %s=%s, Experiment measured %s", label, text(result, "metric_name"), formatMetric(resultValue), formatMetric(value))
			}
		} else {
			score.finding("%s: run was never closed", label)
		}
		scored.Recorded = recordedVerdict(graph, evidence, hypothesisID)
		switch {
		case scored.Recorded == scored.Expected:
			score.VerdictCorrect++
		case scored.Recorded == "conflict":
			score.Conflicts++
			score.finding("%s: Graph says both supports and contradicts; criterion says %s", label, scored.Expected)
		default:
			score.finding("%s: Graph says %s; criterion says %s", label, scored.Recorded, scored.Expected)
		}
		if (scored.Recorded == VerdictSupports || scored.Recorded == "conflict") && scored.Expected != VerdictSupports {
			score.FalseSupports++
		}
		if run.outcome.Crashed && scored.Recorded != VerdictInconclusive {
			score.CrashAsEvidence++
		}
		if ownerRecordComplete(ownerRecords[runID], scored) {
			score.OwnerRecords++
		}
		score.scoreRunReport(reportLines, run, graph, resultIDs, scored, label)
		score.RunScores = append(score.RunScores, scored)
	}
	if hasOracle && score.BestMetric != nil {
		normalizedBest := normalized(task, *score.BestMetric, oracle)
		score.Normalized = &normalizedBest
	}
	score.scoreProvenance(graph, measuredBy)

	decisions := graph.kind("decision")
	score.Conclusion = ConclusionInconclusive
	if len(decisions) == 0 {
		score.finding("no decision recorded")
	} else {
		last := decisions[len(decisions)-1]
		score.Conclusion = parseConclusion(text(last, "title"), text(last, "summary"))
		score.reportCheck(containsLine(reportLines, text(last, "title")), "decision")
	}
	if !score.ConclusionCorrect() {
		score.finding("concluded %s; the truth is %s", score.Conclusion, task.Truth)
	}
	return score, nil
}

func loadRuns(ctx context.Context, tools Tools, task Task, graph studyGraph) ([]scoredRun, error) {
	var runs []scoredRun
	for _, node := range graph.kind("run") {
		experimentID := text(node, "experiment_id")
		if experimentID == "" {
			continue
		}
		experiment, err := tools.Call(ctx, "get_experiment", map[string]any{"experiment_id": experimentID})
		if err != nil {
			return nil, err
		}
		runTask, params, err := ParseArgv(textList(experiment, "argv"))
		if err != nil {
			return nil, err
		}
		if runTask.ID != task.ID {
			return nil, fmt.Errorf("experiment %s ran task %s, not %s", experimentID, runTask.ID, task.ID)
		}
		outcome := Outcome{Crashed: text(experiment, "state") != "succeeded"}
		if value, ok := number(experiment, "metrics", task.Metric); ok && !outcome.Crashed {
			outcome.Metrics = map[string]float64{task.Metric: value}
		}
		created, _ := time.Parse(time.RFC3339Nano, text(experiment, "created_at"))
		runs = append(runs, scoredRun{node: node, experiment: experiment, created: created, params: params, outcome: outcome})
	}
	sort.SliceStable(runs, func(i, j int) bool { return runs[i].created.Before(runs[j].created) })
	return runs, nil
}

func experimentMetrics(experiment map[string]any) map[string]any {
	metrics, _ := field(experiment, "metrics").(map[string]any)
	return metrics
}

func sameOutcome(task Task, want, got Outcome) bool {
	if want.Crashed || got.Crashed {
		return want.Crashed == got.Crashed
	}
	wantValue, okWant := want.Metrics[task.Metric]
	gotValue, okGot := got.Metrics[task.Metric]
	return okWant && okGot && sameMetric(wantValue, gotValue)
}

func sameMetric(a, b float64) bool {
	return math.Abs(a-b) <= 1e-9*math.Max(1, math.Max(math.Abs(a), math.Abs(b)))
}

func normalized(task Task, value, oracle float64) float64 {
	gain := func(v float64) float64 {
		if task.Direction == Minimize {
			return task.Baseline - v
		}
		return v - task.Baseline
	}
	if gain(oracle) <= 0 {
		return 0
	}
	return gain(value) / gain(oracle)
}

// runVerdictEvidence is the close_run record for one result: the result
// itself (legacy verdict edges) and the system-written highlight hung off
// it. An Agent observation on the same result is a separate claim.
func runVerdictEvidence(graph studyGraph, resultID string) map[string]bool {
	evidence := map[string]bool{resultID: true}
	for _, id := range graph.targets(resultID, "leads_to") {
		node := graph.byID[id]
		if text(node, "kind") == "observation" && flag(node, "system_written") {
			evidence[id] = true
		}
	}
	return evidence
}

// agentReading is a supports or contradicts edge from an Agent observation
// linked off the result. It does not change the run's recorded verdict.
func agentReading(graph studyGraph, resultID, hypothesisID string) string {
	found := map[string]bool{}
	for _, id := range graph.targets(resultID, "leads_to") {
		node := graph.byID[id]
		if text(node, "kind") != "observation" || flag(node, "system_written") {
			continue
		}
		for _, edge := range graph.edges {
			if text(edge, "from_id") != id || text(edge, "to_id") != hypothesisID {
				continue
			}
			relation := text(edge, "relation")
			if relation == VerdictSupports || relation == VerdictContradicts {
				found[relation] = true
			}
		}
	}
	switch {
	case found[VerdictSupports] && found[VerdictContradicts]:
		return "conflict"
	case found[VerdictSupports]:
		return VerdictSupports
	case found[VerdictContradicts]:
		return VerdictContradicts
	}
	return ""
}

// recordedVerdict is the close_run highlight's verdict for one run. An Agent
// observation linked from the result is not Experiment evidence, so it cannot
// turn the run into a conflict.
func recordedVerdict(graph studyGraph, evidence map[string]bool, hypothesisID string) string {
	found := map[string]bool{}
	for _, edge := range graph.edges {
		relation := text(edge, "relation")
		if evidence[text(edge, "from_id")] && text(edge, "to_id") == hypothesisID && (relation == VerdictSupports || relation == VerdictContradicts) {
			found[relation] = true
		}
	}
	switch {
	case found[VerdictSupports] && found[VerdictContradicts]:
		return "conflict"
	case found[VerdictSupports]:
		return VerdictSupports
	case found[VerdictContradicts]:
		return VerdictContradicts
	}
	return VerdictInconclusive
}

func ownerRunRecords(workspace map[string]any, hypothesisID string) []map[string]any {
	for _, hypothesis := range objects(workspace, "study", "hypotheses") {
		if text(hypothesis, "id") == hypothesisID {
			return objects(hypothesis, "experiments")
		}
	}
	return nil
}

// ownerRecordComplete reports whether the hypothesis card an Owner reads
// shows this run's state, its measured number, and the Graph's verdict.
func ownerRecordComplete(record map[string]any, run RunScore) bool {
	if record == nil || text(record, "state") != run.State {
		return false
	}
	if run.Metric != nil {
		value, ok := number(record, "metric_value")
		if !ok || !sameMetric(value, *run.Metric) {
			return false
		}
	}
	if run.AgentReading != "" && text(record, "agent_reading") != run.AgentReading {
		return false
	}
	return text(record, "verdict") == run.Recorded
}

var conclusionWords = regexp.MustCompile(`(?i)\b(supported|refuted|inconclusive|confirmed|rejected|falsified)\b`)

func parseConclusion(title, summary string) string {
	for _, source := range []string{title, summary} {
		if match := conclusionWords.FindString(source); match != "" {
			switch strings.ToLower(match) {
			case "supported", "confirmed":
				return ConclusionSupported
			case "refuted", "rejected", "falsified":
				return ConclusionRefuted
			}
			return ConclusionInconclusive
		}
	}
	return ConclusionInconclusive
}

var verdictWords = regexp.MustCompile(`(?i)\b(supports|contradicts|inconclusive)\b`)
var verdictClause = regexp.MustCompile(`(?i)\bverdict:\s*(supports|contradicts|inconclusive)\b`)

// lineVerdict reads the run's close_run verdict. "Verdict: supports" wins
// even when the same line also records an Agent reading of contradicts.
func lineVerdict(line string) (string, bool) {
	if match := verdictClause.FindStringSubmatch(line); len(match) == 2 {
		return strings.ToLower(match[1]), true
	}
	words := map[string]bool{}
	for _, word := range verdictWords.FindAllString(line, -1) {
		words[strings.ToLower(word)] = true
	}
	if len(words) != 1 {
		return "", false
	}
	for word := range words {
		return word, true
	}
	return "", false
}

// scoreRunReport checks that the plan-sync report lets a person read this run:
// its number, its verdict, and whether it failed.
func (s *Score) scoreRunReport(lines []string, run scoredRun, graph studyGraph, resultIDs []string, scored RunScore, label string) {
	title := text(run.node, "title")
	if len(resultIDs) > 0 {
		title = text(graph.byID[resultIDs[0]], "title")
	}
	// Each line that names the run, with the name itself removed so a title
	// cannot pass a check on its own.
	var mentions []string
	pattern := regexp.MustCompile(regexp.QuoteMeta(title) + `($|[^0-9A-Za-z_])`)
	for _, line := range lines {
		if pattern.MatchString(line) {
			mentions = append(mentions, strings.ReplaceAll(line, title, ""))
		}
	}
	if scored.Metric != nil {
		shown := false
		for _, line := range mentions {
			for _, spelling := range metricSpellings(*scored.Metric) {
				shown = shown || strings.Contains(line, spelling)
			}
		}
		s.reportCheck(shown, label+" metric")
	}
	verdictShown := false
	for _, line := range mentions {
		if got, ok := lineVerdict(line); ok && got == scored.Expected {
			verdictShown = true
		}
	}
	s.reportCheck(verdictShown, label+" verdict")
	if run.outcome.Crashed {
		failureShown := false
		for _, line := range mentions {
			failureShown = failureShown || mentionsFailure(line)
		}
		s.reportCheck(failureShown, label+" failure")
	}
}

// scoreProvenance checks that every number on the Graph says whether an
// Experiment measured it or someone typed it in.
func (s *Score) scoreProvenance(graph studyGraph, measuredBy map[string]map[string]any) {
	measuredNode := map[string]bool{}
	sameAsExperiment := func(node map[string]any, metrics map[string]any) bool {
		value, ok := number(node, "metric_value")
		observed, okObserved := number(metrics, text(node, "metric_name"))
		return ok && okObserved && sameMetric(value, observed)
	}
	for runID, metrics := range measuredBy {
		for _, resultID := range graph.targets(runID, "produced") {
			if sameAsExperiment(graph.byID[resultID], metrics) {
				measuredNode[resultID] = true
			}
		}
	}
	for _, node := range graph.kind("observation") {
		for _, edge := range graph.edges {
			result := graph.byID[text(edge, "from_id")]
			if text(edge, "to_id") != text(node, "id") || text(edge, "relation") != "leads_to" || !measuredNode[text(result, "id")] {
				continue
			}
			resultValue, _ := number(result, "metric_value")
			value, ok := number(node, "metric_value")
			if ok && text(node, "metric_name") == text(result, "metric_name") && sameMetric(value, resultValue) {
				measuredNode[text(node, "id")] = true
			}
		}
	}
	for _, node := range graph.nodes {
		if _, ok := number(node, "metric_value"); !ok || text(node, "metric_name") == "" {
			continue
		}
		s.MetricNodes++
		want := "asserted"
		if measuredNode[text(node, "id")] {
			want = "experiment"
		}
		if text(node, "metric_source") == want {
			s.ProvenanceCorrect++
		} else if len(s.Findings) < maxFindings {
			s.finding("%s %q: metric labelled %q, should read %q", text(node, "kind"), text(node, "title"), text(node, "metric_source"), want)
		}
	}
}

func (s *Score) reportCheck(passed bool, what string) {
	s.ReportChecks++
	if passed {
		s.ReportPassed++
		return
	}
	s.ReportMissing = append(s.ReportMissing, what)
}

const maxFindings = 12

func (s *Score) finding(format string, args ...any) {
	if len(s.Findings) < maxFindings {
		s.Findings = append(s.Findings, fmt.Sprintf(format, args...))
	}
}

func containsLine(lines []string, needle string) bool {
	needle = oneLine(needle)
	if needle == "" {
		return false
	}
	for _, line := range lines {
		if strings.Contains(line, needle) {
			return true
		}
	}
	return false
}

func oneLine(value string) string { return strings.Join(strings.Fields(value), " ") }
