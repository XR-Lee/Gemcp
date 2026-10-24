// Package researchbench is a benchmark for automated research through Gemcp.
//
// It poses small simulated research questions with known answers, lets an
// Agent work through the real MCP tools under a run budget, and scores the
// result: did the Agent explore efficiently and reach the right conclusion,
// and do the Graph, the Owner view, and the plan-sync report record what
// actually happened. The scorer reads only MCP, so it can grade any Agent on a
// live control plane as well as the scripted Agents in the tests.
package researchbench

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const (
	VerdictSupports     = "supports"
	VerdictContradicts  = "contradicts"
	VerdictInconclusive = "inconclusive"

	ConclusionSupported    = "supported"
	ConclusionRefuted      = "refuted"
	ConclusionInconclusive = "inconclusive"
)

// Direction says which way a metric improves.
type Direction string

const (
	Maximize Direction = "maximize"
	Minimize Direction = "minimize"
)

// Claim decides what one run can say about a hypothesis. An existence claim
// ("some setting beats the baseline") is supported by one good run, and a weak
// run is no evidence against it. An effect claim ("this change helps") is
// contradicted by a run that does not beat the baseline.
type Claim string

const (
	ExistenceClaim Claim = "existence"
	EffectClaim    Claim = "effect"
)

// Style is how a task is meant to be explored.
type Style string

const (
	// Search tasks look for the best value of one knob inside a range.
	Search Style = "search"
	// Replicate tasks repeat one treatment across seeds.
	Replicate Style = "replicate"
)

// MinReplicates is how many seeds an effect claim needs before a careful
// Agent concludes from the mean instead of one run.
const MinReplicates = 3

type Parameter struct {
	Name    string
	Flag    string
	Min     int
	Max     int
	Default int
}

// Outcome is what one simulated Experiment produces.
type Outcome struct {
	Crashed bool
	Metrics map[string]float64
}

type Task struct {
	// ID is also the gemcp.yaml workload name and the --task value.
	ID         string
	Question   string
	Hypothesis string
	// Criterion is the decision rule the Agent is given; it is also the
	// hypothesis summary on the Graph.
	Criterion string
	// Prior is an unverified outside claim the Agent records before running
	// anything; PriorValue is the number it quotes.
	Prior      string
	PriorValue float64
	Metric     string
	Direction  Direction
	Claim      Claim
	Style      Style
	// Baseline is the reference configuration's metric the criterion quotes;
	// Threshold is the value a run must reach to support the claim.
	Baseline   float64
	Threshold  float64
	Parameters []Parameter
	// Knob and KnobMin/KnobMax bound a search task; Treatment is the setting
	// a replicate task repeats over seeds.
	Knob      string
	KnobMin   int
	KnobMax   int
	Treatment map[string]int
	Budget    int
	// Truth is the correct study-level conclusion.
	Truth    string
	simulate func(params map[string]int) Outcome
}

var (
	lrJitter      = map[int]float64{1: 0.0, 2: 0.0006, 3: -0.0004, 4: 0.0003, 5: -0.0002}
	augmentOffset = map[int]float64{1: 0.016, 2: -0.006, 3: 0.001, 4: -0.011, 5: 0.0}
	depthJitter   = map[int]float64{1: 0.0, 2: 0.002, 3: -0.001, 4: 0.001, 5: -0.002}
	batchJitter   = map[int]float64{1: 0.0, 2: 0.001, 3: -0.001, 4: 0.0005, 5: -0.0005}
)

// augmentEffect is the true effect of augmentation on accuracy.
const augmentEffect = 0.0

var seedParameter = Parameter{Name: "seed", Flag: "--seed", Min: 1, Max: 5, Default: 1}

// Tasks returns the benchmark suite. Each formula mirrors
// examples/research-bench/bench_task.py operation for operation.
func Tasks() []Task {
	return []Task{
		{
			ID:         "lr-sweep",
			Question:   "Which learning rate gives the best validation accuracy, and does tuning beat the default?",
			Hypothesis: "Tuning the learning rate beats the default by at least 0.01 accuracy",
			Criterion:  "A run supports the claim when val_accuracy reaches 0.8128 (default log_lr=-40 scores 0.8028). A weaker run is no evidence against it.",
			Prior:      "A blog post reports 0.93 accuracy with a tuned learning rate", PriorValue: 0.93,
			Metric: "val_accuracy", Direction: Maximize, Claim: ExistenceClaim, Style: Search,
			Baseline: 0.8028, Threshold: 0.8128,
			Parameters: []Parameter{{Name: "log_lr", Flag: "--log-lr", Min: -50, Max: -5, Default: -40}, seedParameter},
			Knob:       "log_lr", KnobMin: -50, KnobMax: -5,
			Budget: 12, Truth: ConclusionSupported,
			simulate: func(p map[string]int) Outcome {
				d := float64(p["log_lr"]+27) / 2
				return measured("val_accuracy", 0.80+0.12/(1+d*d)+lrJitter[p["seed"]])
			},
		},
		{
			ID:         "augment-noise",
			Question:   "Does data augmentation raise validation accuracy?",
			Hypothesis: "Augmentation raises validation accuracy to at least 0.86",
			Criterion:  "Baseline accuracy without augmentation is 0.85. A run supports the claim at 0.86 or more and contradicts it at 0.85 or less. Seeds vary by about 0.01, so conclude from at least 3 seeds.",
			Prior:      "A forum thread reports a 2 point gain from augmentation", PriorValue: 0.87,
			Metric: "val_accuracy", Direction: Maximize, Claim: EffectClaim, Style: Replicate,
			Baseline: 0.85, Threshold: 0.86,
			Parameters: []Parameter{{Name: "augment", Flag: "--augment", Min: 0, Max: 1, Default: 0}, seedParameter},
			Treatment:  map[string]int{"augment": 1},
			Budget:     6, Truth: ConclusionRefuted,
			simulate: func(p map[string]int) Outcome {
				return measured("val_accuracy", 0.85+augmentEffect*float64(p["augment"])+augmentOffset[p["seed"]])
			},
		},
		{
			ID:         "depth-false",
			Question:   "Does a deeper model lower validation loss?",
			Hypothesis: "Depth 4 lowers validation loss to 0.38 or less",
			Criterion:  "Baseline depth 2 has val_loss 0.40; lower is better. A run supports the claim at 0.38 or less and contradicts it at 0.40 or more.",
			Prior:      "A paper reports that depth 4 cut loss by 10 percent", PriorValue: 0.36,
			Metric: "val_loss", Direction: Minimize, Claim: EffectClaim, Style: Replicate,
			Baseline: 0.40, Threshold: 0.38,
			Parameters: []Parameter{{Name: "depth", Flag: "--depth", Min: 1, Max: 8, Default: 2}, seedParameter},
			Treatment:  map[string]int{"depth": 4},
			Budget:     4, Truth: ConclusionRefuted,
			simulate: func(p map[string]int) Outcome {
				if p["depth"] < 2 {
					return measured("val_loss", 0.43+depthJitter[p["seed"]])
				}
				return measured("val_loss", 0.40+0.03*float64(p["depth"]-2)+depthJitter[p["seed"]])
			},
		},
		{
			ID:         "batch-crash",
			Question:   "Can a batch of 2^7 or larger train to at least 0.875 accuracy?",
			Hypothesis: "A batch of at least 2^7 reaches 0.875 validation accuracy",
			Criterion:  "Baseline batch 2^5 scores 0.86. A run supports the claim at 0.875 or more. Large batches may run out of memory; a crash is no evidence either way.",
			Prior:      "A vendor benchmark trains batch 2^9 on the same GPU", PriorValue: 0.89,
			Metric: "val_accuracy", Direction: Maximize, Claim: ExistenceClaim, Style: Search,
			Baseline: 0.86, Threshold: 0.875,
			Parameters: []Parameter{{Name: "batch_log2", Flag: "--batch-log2", Min: 4, Max: 9, Default: 5}, seedParameter},
			Knob:       "batch_log2", KnobMin: 7, KnobMax: 9,
			Budget: 4, Truth: ConclusionSupported,
			simulate: func(p map[string]int) Outcome {
				if p["batch_log2"] >= 8 {
					return Outcome{Crashed: true}
				}
				return measured("val_accuracy", 0.86+0.01*float64(p["batch_log2"]-5)+batchJitter[p["seed"]])
			},
		},
	}
}

func measured(metric string, value float64) Outcome {
	return Outcome{Metrics: map[string]float64{metric: value}}
}

// TaskByID returns one task of the suite.
func TaskByID(id string) (Task, bool) {
	for _, task := range Tasks() {
		if task.ID == id {
			return task, true
		}
	}
	return Task{}, false
}

// Simulate runs the hidden experiment for a complete parameter set.
func (t Task) Simulate(params map[string]int) Outcome {
	return t.simulate(t.withDefaults(params))
}

func (t Task) withDefaults(params map[string]int) map[string]int {
	complete := map[string]int{}
	for _, parameter := range t.Parameters {
		complete[parameter.Name] = parameter.Default
	}
	for name, value := range params {
		complete[name] = value
	}
	return complete
}

// Better reports whether a beats b in the task's direction.
func (t Task) Better(a, b float64) bool {
	if t.Direction == Minimize {
		return a < b
	}
	return a > b
}

func (t Task) reaches(value, target float64) bool {
	if t.Direction == Minimize {
		return value <= target
	}
	return value >= target
}

// ExpectedVerdict is the per-run verdict the task's criterion assigns to an
// observed outcome. A crash is never evidence.
func (t Task) ExpectedVerdict(outcome Outcome) string {
	if outcome.Crashed {
		return VerdictInconclusive
	}
	value, ok := outcome.Metrics[t.Metric]
	if !ok {
		return VerdictInconclusive
	}
	if t.reaches(value, t.Threshold) {
		return VerdictSupports
	}
	if t.Claim == EffectClaim && !t.Better(value, t.Baseline) {
		return VerdictContradicts
	}
	return VerdictInconclusive
}

// Oracle is the best metric reachable anywhere in a search task's knob range
// (seed 1). Replicate tasks have no oracle.
func (t Task) Oracle() (float64, map[string]int, bool) {
	if t.Style != Search {
		return 0, nil, false
	}
	found := false
	var best float64
	var bestParams map[string]int
	for value := t.KnobMin; value <= t.KnobMax; value++ {
		params := map[string]int{t.Knob: value}
		outcome := t.Simulate(params)
		metric, ok := outcome.Metrics[t.Metric]
		if outcome.Crashed || !ok {
			continue
		}
		if !found || t.Better(metric, best) {
			found, best, bestParams = true, metric, params
		}
	}
	return best, bestParams, found
}

// Argv is the command line the workload in gemcp.yaml produces for params.
func (t Task) Argv(params map[string]int) []string {
	argv := []string{"python3", "bench_task.py", "--task", t.ID}
	complete := t.withDefaults(params)
	for _, parameter := range t.Parameters {
		argv = append(argv, parameter.Flag, strconv.Itoa(complete[parameter.Name]))
	}
	return argv
}

// WorkloadParameters is the prepare_experiment parameters object for params.
func (t Task) WorkloadParameters(params map[string]int) map[string]any {
	values := map[string]any{}
	for name, value := range t.withDefaults(params) {
		values[name] = strconv.Itoa(value)
	}
	return values
}

// ParseArgv recovers the task and parameters from an Experiment's argv, so a
// run can be re-simulated from what Gemcp actually executed.
func ParseArgv(argv []string) (Task, map[string]int, error) {
	taskID := ""
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == "--task" {
			taskID = argv[i+1]
		}
	}
	task, ok := TaskByID(taskID)
	if !ok {
		return Task{}, nil, fmt.Errorf("argv names no benchmark task: %q", strings.Join(argv, " "))
	}
	params := map[string]int{}
	for _, parameter := range task.Parameters {
		for i := 0; i+1 < len(argv); i++ {
			if argv[i] != parameter.Flag {
				continue
			}
			value, err := strconv.Atoi(argv[i+1])
			if err != nil {
				return Task{}, nil, fmt.Errorf("argv %s value %q is not an integer", parameter.Flag, argv[i+1])
			}
			params[parameter.Name] = value
		}
	}
	return task, task.withDefaults(params), nil
}

// DescribeParams renders params in a stable order, for titles.
func (t Task) DescribeParams(params map[string]int) string {
	complete := t.withDefaults(params)
	names := make([]string, 0, len(complete))
	for name := range complete {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, name+"="+strconv.Itoa(complete[name]))
	}
	return strings.Join(parts, " ")
}
