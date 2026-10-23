package researchbench

import (
	"fmt"
	"strings"
)

// Report is one benchmark pass over a Gemcp build.
type Report struct {
	Label     string    `json:"label"`
	Scores    []Score   `json:"scores"`
	Integrity Integrity `json:"integrity"`
}

// Summary adds up the scores of one Agent, or of every Agent when Agent is
// "all".
type Summary struct {
	Agent              string
	Episodes           int
	ConclusionsCorrect int
	SearchEpisodes     int
	SearchSolved       int
	NormalizedSum      float64
	RunsToTargetSum    int
	Runs               int
	Budget             int
	Crashes            int
	Duplicates         int
	Closed             int
	LabMismatches      int
	MetricMismatches   int
	VerdictCorrect     int
	FalseSupports      int
	CrashAsEvidence    int
	Conflicts          int
	CriterionKept      int
	MetricNodes        int
	ProvenanceCorrect  int
	OwnerRecords       int
	ReportChecks       int
	ReportPassed       int
	CrashPrompts       int
	MisleadingPrompts  int
	LegacyVerdicts     int
	ToolCalls          int
	ResponseBytes      int
}

func (s *Summary) add(score Score) {
	s.Episodes++
	if score.ConclusionCorrect() {
		s.ConclusionsCorrect++
	}
	if score.Normalized != nil {
		s.SearchEpisodes++
		s.NormalizedSum += *score.Normalized
		if *score.Normalized >= TargetScore {
			s.SearchSolved++
			s.RunsToTargetSum += score.RunsToTarget
		}
	} else if task, ok := TaskByID(score.Task); ok && task.Style == Search {
		s.SearchEpisodes++
	}
	s.Runs += score.Runs
	s.Budget += score.Budget
	s.Crashes += score.Crashes
	s.Duplicates += score.Duplicates
	s.Closed += score.Closed
	s.LabMismatches += score.LabMismatches
	s.MetricMismatches += score.MetricMismatches
	s.VerdictCorrect += score.VerdictCorrect
	s.FalseSupports += score.FalseSupports
	s.CrashAsEvidence += score.CrashAsEvidence
	s.Conflicts += score.Conflicts
	if score.CriterionKept {
		s.CriterionKept++
	}
	s.MetricNodes += score.MetricNodes
	s.ProvenanceCorrect += score.ProvenanceCorrect
	s.OwnerRecords += score.OwnerRecords
	s.ReportChecks += score.ReportChecks
	s.ReportPassed += score.ReportPassed
	s.CrashPrompts += score.CrashPrompts
	s.MisleadingPrompts += score.MisleadingPrompts
	s.LegacyVerdicts += score.LegacyVerdicts
	s.ToolCalls += score.ToolCalls
	s.ResponseBytes += score.ResponseBytes
}

// Summaries returns one Summary per Agent in first-seen order, then "all".
func (r Report) Summaries() []Summary {
	var summaries []Summary
	index := map[string]int{}
	all := Summary{Agent: "all"}
	for _, score := range r.Scores {
		position, ok := index[score.Agent]
		if !ok {
			position = len(summaries)
			index[score.Agent] = position
			summaries = append(summaries, Summary{Agent: score.Agent})
		}
		summaries[position].add(score)
		all.add(score)
	}
	return append(summaries, all)
}

func ratio(numerator, denominator int) string {
	if denominator == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%d/%d (%.0f%%)", numerator, denominator, 100*float64(numerator)/float64(denominator))
}

func perRun(total, runs int, unit float64, format string) string {
	if runs == 0 {
		return "n/a"
	}
	return fmt.Sprintf(format, float64(total)/float64(runs)/unit)
}

type metricRow struct {
	name   string
	render func(Summary) string
}

// agentRows grade the Agent; they do not depend on how Gemcp records the
// evidence.
var agentRows = []metricRow{
	{"Correct study conclusions", func(s Summary) string { return ratio(s.ConclusionsCorrect, s.Episodes) }},
	{"Search tasks solved (S ≥ 0.95)", func(s Summary) string { return ratio(s.SearchSolved, s.SearchEpisodes) }},
	{"Mean normalized score S", func(s Summary) string {
		if s.SearchEpisodes == 0 {
			return "n/a"
		}
		return fmt.Sprintf("%.2f", s.NormalizedSum/float64(s.SearchEpisodes))
	}},
	{"Mean runs to target (solved tasks)", func(s Summary) string {
		if s.SearchSolved == 0 {
			return "n/a"
		}
		return fmt.Sprintf("%.1f", float64(s.RunsToTargetSum)/float64(s.SearchSolved))
	}},
	{"Runs used / budget", func(s Summary) string { return fmt.Sprintf("%d/%d", s.Runs, s.Budget) }},
	{"Duplicate runs", func(s Summary) string { return fmt.Sprint(s.Duplicates) }},
}

// recordRows grade what Gemcp keeps: the Graph, the Owner's hypothesis
// cards, the plan-sync report, and the guidance it gives the Agent.
var recordRows = []metricRow{
	{"Run verdicts recorded correctly", func(s Summary) string { return ratio(s.VerdictCorrect, s.Runs) }},
	{"False `supports` on the Graph", func(s Summary) string { return fmt.Sprint(s.FalseSupports) }},
	{"Crashed runs recorded as evidence", func(s Summary) string { return ratio(s.CrashAsEvidence, s.Crashes) }},
	{"Runs with conflicting verdicts", func(s Summary) string { return fmt.Sprint(s.Conflicts) }},
	{"Runs closed", func(s Summary) string { return ratio(s.Closed, s.Runs) }},
	{"Result metric equals the Experiment's", func(s Summary) string { return ratio(s.Closed-s.MetricMismatches, s.Closed) }},
	{"Hypothesis criterion kept", func(s Summary) string { return ratio(s.CriterionKept, s.Episodes) }},
	{"Numbers labelled measured vs asserted", func(s Summary) string { return ratio(s.ProvenanceCorrect, s.MetricNodes) }},
	{"Owner hypothesis cards show metric + verdict", func(s Summary) string { return ratio(s.OwnerRecords, s.Runs) }},
	{"Plan-sync report checks passed", func(s Summary) string { return ratio(s.ReportPassed, s.ReportChecks) }},
	{"Misleading next step after a crash", func(s Summary) string { return ratio(s.MisleadingPrompts, s.CrashPrompts) }},
	{"Verdict workarounds needed (close_run without verdict)", func(s Summary) string { return fmt.Sprint(s.LegacyVerdicts) }},
	{"Tool calls per run", func(s Summary) string { return perRun(s.ToolCalls, s.Runs, 1, "%.1f") }},
	{"Response KB per run", func(s Summary) string { return perRun(s.ResponseBytes, s.Runs, 1024, "%.1f") }},
	{"Lab mismatches (harness self-check)", func(s Summary) string { return fmt.Sprint(s.LabMismatches) }},
}

func table(b *strings.Builder, header []string, rows [][]string) {
	b.WriteString("| " + strings.Join(header, " | ") + " |\n")
	b.WriteString("|" + strings.Repeat(" --- |", len(header)) + "\n")
	for _, row := range rows {
		b.WriteString("| " + strings.Join(row, " | ") + " |\n")
	}
}

func metricTable(b *strings.Builder, rows []metricRow, summaries []Summary) {
	header := []string{"Metric"}
	for _, summary := range summaries {
		header = append(header, summary.Agent)
	}
	var cells [][]string
	for _, row := range rows {
		line := []string{row.name}
		for _, summary := range summaries {
			line = append(line, row.render(summary))
		}
		cells = append(cells, line)
	}
	table(b, header, cells)
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

// RenderMarkdown is the human report for one pass.
func RenderMarkdown(report Report) string {
	var b strings.Builder
	summaries := report.Summaries()
	fmt.Fprintf(&b, "# Research benchmark: %s\n\n## Agents\n\n", report.Label)
	metricTable(&b, agentRows, summaries)
	b.WriteString("\n## Records\n\n")
	metricTable(&b, recordRows, summaries)
	b.WriteString("\n## Episodes\n\n")
	var rows [][]string
	for _, score := range report.Scores {
		best, normalizedScore := "n/a", "n/a"
		if score.BestMetric != nil {
			best = formatMetric(*score.BestMetric)
		}
		if score.Normalized != nil {
			normalizedScore = fmt.Sprintf("%.2f", *score.Normalized)
		}
		conclusion := score.Conclusion
		if !score.ConclusionCorrect() {
			conclusion += " ✗ (truth: " + score.Truth + ")"
		}
		rows = append(rows, []string{
			score.Agent, score.Task, fmt.Sprintf("%d/%d", score.Runs, score.Budget), best, normalizedScore,
			conclusion, ratio(score.VerdictCorrect, score.Runs), ratio(score.ReportPassed, score.ReportChecks),
		})
	}
	table(&b, []string{"Agent", "Task", "Runs", "Best", "S", "Conclusion", "Verdicts right", "Report checks"}, rows)
	if len(report.Integrity.Probes) > 0 {
		b.WriteString("\n## Evidence integrity\n\n")
		rows = nil
		for _, probe := range report.Integrity.Probes {
			rows = append(rows, []string{probe.Name, yesNo(probe.Accepted), yesNo(probe.Effective)})
		}
		table(&b, []string{"Rewrite attempt", "Accepted", "Changed the record"}, rows)
		fmt.Fprintf(&b, "\nRuns one hypothesis held before Gemcp refused another: %d (limit hit: %s, probe limit %d; refused by %s; the Agent was told %s). Runs spent without a Graph run: %d. Out-of-range parameter accepted: %s. Agent could submit a proposal the Owner never confirmed: %s.\n",
			report.Integrity.Capacity, yesNo(report.Integrity.CapacityHit), report.Integrity.CapacityLimit, codeText(report.Integrity.RefusedBy),
			refusalText(report.Integrity.Refusal), report.Integrity.Orphaned, yesNo(report.Integrity.OutOfBounds), yesNo(report.Integrity.UnapprovedSubmit))
	}
	b.WriteString("\n## Findings\n\n")
	listed := false
	for _, score := range report.Scores {
		for i, finding := range score.Findings {
			if i == 3 {
				fmt.Fprintf(&b, "- %s/%s: … %d more\n", score.Agent, score.Task, len(score.Findings)-3)
				break
			}
			fmt.Fprintf(&b, "- %s/%s: %s\n", score.Agent, score.Task, finding)
			listed = true
		}
	}
	if !listed {
		b.WriteString("None.\n")
	}
	return b.String()
}

// RenderComparison puts passes side by side, oldest first, metric by metric
// over all Agents.
func RenderComparison(reports ...Report) string {
	var b strings.Builder
	header := []string{"Metric"}
	for _, report := range reports {
		header = append(header, report.Label)
	}
	integrityRows := []struct {
		name   string
		render func(Integrity) string
	}{
		{"Evidence rewrites that changed the record", func(i Integrity) string { return fmt.Sprintf("%d/%d", i.Effective(), len(i.Probes)) }},
		{"Runs one hypothesis holds", func(i Integrity) string { return fmt.Sprint(i.Capacity) }},
		{"Tool that refuses the next run on a full Study", func(i Integrity) string { return codeText(i.RefusedBy) }},
		{"What the Agent is told when the Study is full", func(i Integrity) string { return refusalText(i.Refusal) }},
		{"Out-of-range parameter accepted by prepare_experiment", func(i Integrity) string { return yesNo(i.OutOfBounds) }},
		{"Unconfirmed proposal submitted by the Agent", func(i Integrity) string { return yesNo(i.UnapprovedSubmit) }},
	}
	var rows [][]string
	for _, row := range recordRows {
		line := []string{row.name}
		for _, report := range reports {
			line = append(line, row.render(last(report.Summaries())))
		}
		rows = append(rows, line)
	}
	for _, row := range integrityRows {
		line := []string{row.name}
		for _, report := range reports {
			line = append(line, row.render(report.Integrity))
		}
		rows = append(rows, line)
	}
	table(&b, header, rows)
	b.WriteString("\nAgent results per build:\n\n")
	byAgent := make([]map[string]Summary, len(reports))
	for i, report := range reports {
		byAgent[i] = map[string]Summary{}
		for _, summary := range report.Summaries() {
			byAgent[i][summary.Agent] = summary
		}
	}
	rows = nil
	for _, summary := range reports[0].Summaries() {
		for _, row := range agentRows[:3] {
			line := []string{summary.Agent, row.name}
			for i := range reports {
				line = append(line, row.render(byAgent[i][summary.Agent]))
			}
			rows = append(rows, line)
		}
	}
	table(&b, append([]string{"Agent"}, header...), rows)
	return b.String()
}

func last(summaries []Summary) Summary { return summaries[len(summaries)-1] }

// refusalText keeps the tool's own message and drops the call context the
// driver wraps it in.
func refusalText(refusal string) string {
	if index := strings.LastIndex(refusal, ": "); index >= 0 {
		refusal = refusal[index+2:]
	}
	return codeText(refusal)
}

// codeText shows a tool name or message as code, or n/a when there is none,
// as for a pass recorded before the field existed.
func codeText(value string) string {
	if value == "" {
		return "n/a"
	}
	return "`" + value + "`"
}
