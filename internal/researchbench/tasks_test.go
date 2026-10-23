package researchbench

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

// offline plays a policy against the simulator alone. When TestResearchBench
// fails but this agrees with it, the fault is in how Gemcp recorded the work.
func offline(task Task, policy Policy) ([]Run, Conclusion) {
	var runs []Run
	for len(runs) < task.Budget {
		params, more := policy.Next(task, runs)
		if !more {
			break
		}
		outcome := task.Simulate(params)
		run := Run{Params: task.withDefaults(params), Crashed: outcome.Crashed, Verdict: task.ExpectedVerdict(outcome)}
		run.Metric, run.HasMetric = outcome.Metrics[task.Metric]
		runs = append(runs, run)
	}
	return runs, policy.Conclude(task, runs)
}

func meanOverSeeds(task Task, params map[string]int) float64 {
	sum := 0.0
	for seed := seedParameter.Min; seed <= seedParameter.Max; seed++ {
		sum += task.Simulate(withSeed(params, seed)).Metrics[task.Metric]
	}
	return sum / float64(seedParameter.Max-seedParameter.Min+1)
}

func TestTasksHaveTheAnswersTheyClaim(t *testing.T) {
	for _, task := range Tasks() {
		// The criterion quotes the default configuration's mean over seeds.
		if got := formatMetric(meanOverSeeds(task, nil)); got != formatMetric(task.Baseline) {
			t.Errorf("%s: default configuration scores %s, criterion quotes %s", task.ID, got, formatMetric(task.Baseline))
		}
		switch task.Style {
		case Search:
			oracle, _, ok := task.Oracle()
			if !ok || task.reaches(oracle, task.Threshold) != (task.Truth == ConclusionSupported) {
				t.Errorf("%s: oracle %v against threshold %v contradicts truth %s", task.ID, oracle, task.Threshold, task.Truth)
			}
		case Replicate:
			if mean := meanOverSeeds(task, task.Treatment); task.reaches(mean, task.Threshold) != (task.Truth == ConclusionSupported) {
				t.Errorf("%s: treatment mean over every seed %v against threshold %v contradicts truth %s", task.ID, mean, task.Threshold, task.Truth)
			}
		}
	}

	// Each task sets one trap for an Agent that concludes too early.
	lr, _ := TaskByID("lr-sweep")
	oracle, _, _ := lr.Oracle()
	onTarget := 0
	for value := lr.KnobMin; value <= lr.KnobMax; value++ {
		if normalized(lr, lr.Simulate(map[string]int{"log_lr": value}).Metrics[lr.Metric], oracle) >= TargetScore {
			onTarget++
		}
	}
	if onTarget != 1 {
		t.Errorf("lr-sweep: %d knob values are on target, want exactly one narrow optimum", onTarget)
	}
	augment, _ := TaskByID("augment-noise")
	if got := augment.ExpectedVerdict(augment.Simulate(withSeed(augment.Treatment, 1))); got != VerdictSupports {
		t.Errorf("augment-noise: lucky seed 1 reads %s, want a misleading supports", got)
	}
	batch, _ := TaskByID("batch-crash")
	if !batch.Simulate(map[string]int{"batch_log2": batch.KnobMax}).Crashed {
		t.Error("batch-crash: the largest batch must run out of memory")
	}
}

func TestScriptedAgentsOffline(t *testing.T) {
	correct := map[string]int{}
	for _, policy := range []Policy{Careful{}, Grid{}, Hasty{}} {
		for _, task := range Tasks() {
			runs, conclusion := offline(task, policy)
			if conclusion.Verdict == task.Truth {
				correct[policy.Name()]++
			}
			if len(runs) > task.Budget {
				t.Errorf("%s/%s spent %d runs, budget %d", policy.Name(), task.ID, len(runs), task.Budget)
			}
			if policy.Name() == "careful" && task.ID == "lr-sweep" {
				oracle, _, _ := task.Oracle()
				reached := 0
				for i, run := range runs {
					if reached == 0 && run.HasMetric && normalized(task, run.Metric, oracle) >= TargetScore {
						reached = i + 1
					}
				}
				if reached == 0 || reached > task.Budget {
					t.Errorf("careful lr-sweep reached the optimum at run %d of %d", reached, task.Budget)
				}
			}
			if policy.Name() == "grid" && task.ID == "lr-sweep" && conclusion.HasBest {
				oracle, _, _ := task.Oracle()
				if normalized(task, conclusion.BestMetric, oracle) >= TargetScore {
					t.Error("grid lr-sweep hit the narrow optimum; the task no longer rewards adaptive search")
				}
			}
		}
	}
	if correct["careful"] != 4 || correct["grid"] != 4 || correct["hasty"] != 1 {
		t.Errorf("correct conclusions = %v, want careful 4, grid 4, hasty 1", correct)
	}
}

type manifest struct {
	Workloads map[string]struct {
		Entrypoint []string `yaml:"entrypoint"`
		Arguments  []struct {
			Flag      string `yaml:"flag"`
			Parameter string `yaml:"parameter"`
		} `yaml:"arguments"`
		RuntimePreset string `yaml:"runtime_preset"`
		Parameters    map[string]struct {
			Type    string `yaml:"type"`
			Minimum int    `yaml:"minimum"`
			Maximum int    `yaml:"maximum"`
			Default int    `yaml:"default"`
		} `yaml:"parameters"`
		Outputs struct {
			Metrics string `yaml:"metrics"`
		} `yaml:"outputs"`
	} `yaml:"workloads"`
}

func TestWorkloadManifestMatchesTasks(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "examples", "research-bench", "gemcp.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var parsed manifest
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Workloads) != len(Tasks()) {
		t.Errorf("gemcp.yaml has %d workloads, the suite has %d tasks", len(parsed.Workloads), len(Tasks()))
	}
	for _, task := range Tasks() {
		workload, ok := parsed.Workloads[task.ID]
		if !ok {
			t.Errorf("gemcp.yaml has no workload %s", task.ID)
			continue
		}
		if want := task.Argv(nil)[:4]; !reflect.DeepEqual(workload.Entrypoint, want) {
			t.Errorf("%s entrypoint = %v, want %v", task.ID, workload.Entrypoint, want)
		}
		if workload.Outputs.Metrics != "metrics.json" || workload.RuntimePreset != "smoke" {
			t.Errorf("%s outputs/preset = %q/%q", task.ID, workload.Outputs.Metrics, workload.RuntimePreset)
		}
		if len(workload.Arguments) != len(task.Parameters) {
			t.Errorf("%s has %d arguments, want %d", task.ID, len(workload.Arguments), len(task.Parameters))
			continue
		}
		for i, parameter := range task.Parameters {
			argument, declared := workload.Arguments[i], workload.Parameters[parameter.Name]
			if argument.Flag != parameter.Flag || argument.Parameter != parameter.Name || declared.Type != "integer" ||
				declared.Minimum != parameter.Min || declared.Maximum != parameter.Max || declared.Default != parameter.Default {
				t.Errorf("%s parameter %s: yaml %+v / %+v, task %+v", task.ID, parameter.Name, argument, declared, parameter)
			}
		}
	}
}

// TestSimulatorMatchesBenchTask runs examples/research-bench/bench_task.py
// against the Go simulator, so the in-process benchmark and a live deployment
// grade the same experiments.
func TestSimulatorMatchesBenchTask(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not installed")
	}
	script, err := filepath.Abs(filepath.Join("..", "..", "examples", "research-bench", "bench_task.py"))
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range Tasks() {
		var cases []map[string]int
		for _, parameter := range task.Parameters {
			if parameter.Name == "seed" {
				continue
			}
			for value := parameter.Min; value <= parameter.Max; value++ {
				cases = append(cases, map[string]int{parameter.Name: value})
			}
		}
		for seed := seedParameter.Min; seed <= seedParameter.Max; seed++ {
			params := withSeed(task.Treatment, seed)
			if task.Style == Search {
				_, best, _ := task.Oracle()
				params = withSeed(best, seed)
			}
			cases = append(cases, params)
		}
		for _, params := range cases {
			output := t.TempDir()
			command := exec.Command(python, append([]string{script}, task.Argv(params)[2:]...)...)
			command.Env = append(os.Environ(), "GEMCP_OUTPUT_DIR="+output)
			command.Dir = output
			err := command.Run()
			want := task.Simulate(params)
			var exit *exec.ExitError
			if want.Crashed {
				if !errors.As(err, &exit) || exit.ExitCode() != 137 {
					t.Errorf("%s %s: want exit 137, got %v", task.ID, task.DescribeParams(params), err)
				}
				continue
			}
			if err != nil {
				t.Fatalf("%s %s: %v", task.ID, task.DescribeParams(params), err)
			}
			data, err := os.ReadFile(filepath.Join(output, "metrics.json"))
			if err != nil {
				t.Fatal(err)
			}
			var metrics map[string]float64
			if err := json.Unmarshal(data, &metrics); err != nil {
				t.Fatal(err)
			}
			if got := metrics[task.Metric]; got != want.Metrics[task.Metric] {
				t.Errorf("%s %s: bench_task.py %v, Go %v", task.ID, task.DescribeParams(params), got, want.Metrics[task.Metric])
			}
		}
	}
}
