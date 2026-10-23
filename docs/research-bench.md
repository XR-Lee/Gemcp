# Research benchmark

Status: test suite in `internal/researchbench`, run by `make test`. It measures two things on small research questions whose answers are known:

1. **The Agent** — did it explore efficiently inside its constraints and reach the right conclusion?
2. **Gemcp's record** — do the Graph, the Owner's hypothesis cards, and the plan-sync report say what actually happened?

The words below (Study, hypothesis, run, result, highlight, verdict) are the ones in the [Graph contract](graph-contract.md).

## What runs

Each task is a workload in [`examples/research-bench/gemcp.yaml`](../examples/research-bench/gemcp.yaml). [`bench_task.py`](../examples/research-bench/bench_task.py) writes `metrics.json` like a training script, or exits 137 when the simulated job runs out of memory. `internal/researchbench` holds the same formulas in Go; a parity test checks that both produce the same numbers bit for bit.

| Task | Question | Constraint | Trap | Truth |
| --- | --- | --- | --- | --- |
| `lr-sweep` | Does tuning the learning rate beat the default (0.8028) by 0.01? | `log_lr` in [-50, -5], 12 runs | Only `log_lr=-27` reaches 0.92; a coarse grid lands on the shoulders | supported |
| `augment-noise` | Does augmentation raise accuracy to 0.86? | `augment` in {0, 1}, seed 1–5, 6 runs | Seed 1 reads 0.866; the true effect is zero | refuted |
| `depth-false` | Does depth 4 lower `val_loss` to 0.38? | `depth` in [1, 8], 4 runs | A paper claims it does; every seed says the opposite | refuted |
| `batch-crash` | Can a batch of 2^7 or more reach 0.875? | `batch_log2` in [7, 9], 4 runs | 2^8 and 2^9 run out of memory; 2^7 reaches 0.88 | supported |

Every Agent gets the same brief: the question, a hypothesis whose summary is the decision rule (the *criterion*), an unverified outside claim to record as an observation, the workload and its parameter bounds, and the run budget. The parameter bounds are enforced by `prepare_experiment`; the budget is enforced by the Owner, who stops confirming proposals once it is spent.

One episode is one Agent on one task, through the real MCP tools:

1. `update_research_workspace`: create the Study and the hypothesis; record the outside claim and link it to the hypothesis with an edge-only update by `id`.
2. For each run: `prepare_experiment` (`workload`, `parameters`, `from_node_id`, `expected_metric`) → the Owner checks the displayed command and the budget → `submit_prepared_experiment` → the backend runs → `get_experiment` → `close_run` with the criterion's `verdict`. After a crashed run, the Agent reads `get_next_actions`.
3. Record a decision titled `Decision: hypothesis supported|refuted|inconclusive`, then `export_research_plan_sync`.

The MCP Streamable HTTP server, Agent auth, prepared-proposal path (gemcp.yaml from the commit archive, bounds, digest), Graph binder, `close_run`, next actions, and plan sync are production code on SQLite. Git (an archive of `examples/research-bench`), GPU stock, the scheduler, and the Owner are stand-ins: the backend writes the Experiment's terminal state, exit code, log tail, and `metrics.json` the way the scheduler would.

## Scripted Agents

They are reference policies, not language models. They show that the benchmark separates good research from bad, and that Gemcp records each faithfully.

| Agent | Search tasks | Effect claims |
| --- | --- | --- |
| `careful` | Coarse grid on half the budget, then splits the wider gap next to the best point | Three seeds, concludes from the mean |
| `grid` | Evenly spaced grid over the whole budget | Every seed it can afford |
| `hasty` | One run at the edge of the range | One seed; reads a crash as a negative result |

## Metrics

Agent metrics depend only on what the Agent did:

| Metric | Definition |
| --- | --- |
| Correct study conclusions | The last decision node's verdict equals the task's truth |
| Normalized score S | (best − baseline) / (oracle − baseline) on search tasks; the oracle is the best value in the knob range |
| Search tasks solved | S ≥ 0.95 |
| Runs to target | Index of the first run with S ≥ 0.95 |
| Runs used, duplicate runs | Spend against the budget, and runs that repeat a parameter set |

Record metrics are read back through MCP after the episode and do not trust the Agent's account:

| Metric | Definition |
| --- | --- |
| Run verdicts recorded correctly | For each run, the `supports` / `contradicts` edges into the hypothesis from its result, or from any node hanging off that result, equal what the criterion gives the Experiment's measured outcome. None means inconclusive |
| False `supports` | Runs the Graph marks as support that the criterion does not |
| Crashed runs recorded as evidence | Failed runs with any verdict other than inconclusive |
| Conflicting verdicts | Runs marked both `supports` and `contradicts` |
| Result metric equals the Experiment's | The result's metric matches `metrics.json`; a crashed run's result carries none |
| Hypothesis criterion kept | The hypothesis summary still states the criterion at the end |
| Numbers labelled measured vs asserted | Every scalar on the Graph has the right `metric_source` |
| Owner hypothesis cards complete | The hypothesis card's row for the run shows its state, its measured metric, and the Graph's verdict |
| Plan-sync report checks | The report names the hypothesis and criterion, the decision, and for each run its number, its verdict, and whether it failed. A node title alone never satisfies a check |
| Misleading next step after a crash | `get_next_actions` asks for a decision without saying the crash is no evidence |
| Evidence rewrites that changed the record | Five edits a careless or motivated Agent might try after `close_run` (below) |
| Runs one hypothesis holds | Runs closed on one hypothesis before Gemcp refuses another, the tool that refuses it, and the message the Agent gets |
| Cost | Tool calls per run, and JSON bytes of structured tool results per run (the MCP SDK also sends each result as text, so the wire carries about twice that) |

A lab self-check re-simulates every Experiment from its stored argv; any mismatch means the harness, not Gemcp, is wrong.

## Results

Same Agents, same tasks, three builds: `main` before these fixes, the first fix commit (`df0a3bb`), and this branch. The scripted Agents behave the same on every build (careful 4/4 conclusions and both searches solved, S = 1.00; grid 4/4 but S = 0.74; hasty 1/4); only Gemcp's record changes.

| Metric | main | df0a3bb | branch |
| --- | --- | --- | --- |
| Run verdicts recorded correctly | 15/49 (31%) | 49/49 (100%) | 49/49 (100%) |
| False `supports` on the Graph | 29 | 0 | 0 |
| Crashed runs recorded as evidence | 5/5 (100%) | 0/5 (0%) | 0/5 (0%) |
| Runs with conflicting verdicts | 12 | 0 | 0 |
| Runs closed | 49/49 (100%) | 49/49 (100%) | 49/49 (100%) |
| Result metric equals the Experiment's | 49/49 (100%) | 49/49 (100%) | 49/49 (100%) |
| Hypothesis criterion kept | 0/12 (0%) | 12/12 (100%) | 12/12 (100%) |
| Numbers labelled measured vs asserted | 0/100 (0%) | 100/100 (100%) | 100/100 (100%) |
| Owner hypothesis cards show metric + verdict | 0/49 (0%) | 49/49 (100%) | 49/49 (100%) |
| Plan-sync report checks passed | 24/134 (18%) | 134/134 (100%) | 134/134 (100%) |
| Misleading next step after a crash | 5/5 (100%) | 0/5 (0%) | 0/5 (0%) |
| Verdict workarounds needed (close_run without verdict) | 49 | 0 | 0 |
| Tool calls per run | 7.7 | 5.6 | 5.6 |
| Response KB per run | 57.1 | 35.9 | 35.9 |
| Lab mismatches (harness self-check) | 0 | 0 | 0 |
| Evidence rewrites that changed the record | 4/5 | 2/5 | 0/5 |
| Runs one hypothesis holds | 39 | 42 | 42 |
| Tool that refuses the next run on a full Study | `submit_prepared_experiment` | `submit_prepared_experiment` | `prepare_experiment` |
| What the Agent is told when the Study is full | `internal control-plane error` | `internal control-plane error` | `research graph node limit reached` |
| Out-of-range parameter accepted by prepare_experiment | no | no | no |
| Unconfirmed proposal submitted by the Agent | yes | yes | yes |

| Rewrite attempt after `close_run` | main | df0a3bb | branch |
| --- | --- | --- | --- |
| Raise a measured result's metric | refused | refused | refused |
| Retitle a crash highlight as a success | changed | refused | refused |
| Mark a crashed run succeeded | changed | refused | refused |
| Link a crashed result as `supports` | changed | changed | refused |
| Link a crash highlight as `supports` | changed | changed | refused |

What the numbers on `main` come from:

- `close_run` wrote `supports` for every run that finished and `contradicts` for every run that crashed. `main` rejects a `verdict` argument, so the Agents recorded their reading as a separate observation; when the two disagreed, the Graph said both.
- An edge-only update by `id` cleared the hypothesis summary, so the criterion disappeared from all 12 Studies.
- The outside claims were recorded with numbers (0.93, 0.87, 0.36, 0.89) that looked exactly like measured ones; hypothesis cards had no metric or verdict; plan sync listed result titles with "Scalar values stay on the Graph".
- After a crash, `get_next_actions` said "Record whether it supports or contradicts that claim."
- The workaround cost 37% more tool calls and 59% more tool output per run, and the extra notes used Graph capacity (39 runs instead of 42).

The first fix commit left two holes that the benchmark found: an edge-only update could still link a crashed result or its highlight as `supports`, and a full Study surfaced as `internal control-plane error` at submit. Both are fixed on this branch. The capacity probe also showed that a full Study was refused only at `submit_prepared_experiment`, after the Owner had approved the proposal; `prepare_experiment` now runs the same Graph bind check and refuses first, so the Owner never sees it. One hypothesis still holds 42 runs.

### Still open

- **Owner confirmation is not enforced.** `prepare_experiment` returns the digest to the Agent, and `submit_prepared_experiment` accepts it without the Owner. The guide tells Agents to wait; an unattended bot can skip that.
- **Tool output grows with the Graph.** Every Graph write returns the whole workspace: the careful Agent's 12-run `lr-sweep` read 54 responses and 466 KB, about 39 KB per run, which is a real context cost for a language-model Agent.
- **One hypothesis holds about 42 runs.** The 128-node Study cap counts the run, result, and highlight of each run. `prepare_experiment` refuses the next run before the Owner sees it, but the Study takes no more runs.
- **Execution is simulated.** The scheduler, Provider, Runner, and artifacts are not exercised here; use [diagnostics](diagnostics.md) and `./scripts/local-cpu-loop.sh` for those.

## Running it

```bash
go test ./internal/researchbench -run TestResearchBench -v       # report in the log
GEMCP_BENCH_OUT=/tmp/bench GEMCP_BENCH_LABEL=branch \
  go test ./internal/researchbench -run TestResearchBench         # also writes branch.json and branch.md
```

`TestResearchBench` fails when any record metric regresses for any Agent, when one hypothesis holds fewer than 40 runs or a full Study is refused anywhere but `prepare_experiment`, when the careful Agent misses a conclusion or a search, or when the benchmark stops discriminating: the grid Agent solves as many searches as the careful one, or the hasty Agent gets every conclusion right.

To measure an older build, check it out in a worktree, copy `internal/researchbench/` and `examples/research-bench/` into it, and add `GEMCP_BENCH_MEASURE_ONLY=1` so the thresholds are not asserted. Then compare saved passes, oldest first:

```bash
GEMCP_BENCH_COMPARE=/tmp/bench/main.json,/tmp/bench/branch.json GEMCP_BENCH_OUT=/tmp/bench \
  go test ./internal/researchbench -run TestRenderComparison -v
```

The other tests check the benchmark itself: each task's truth follows from its formulas (`TestTasksHaveTheAnswersTheyClaim`), the scripted Agents behave as described without Gemcp (`TestScriptedAgentsOffline`), `gemcp.yaml` matches the Go task definitions, and `bench_task.py` matches the Go simulator (skipped without `python3`).

## Grading a live Agent

The scorer reads only MCP, so it can grade a Study that any Agent ran on a live control plane:

1. Make the workloads runnable. On a repository-backed backend, put the files of `examples/research-bench/` at the root of a registered repository and have the Agent use `workload` and `parameters`. On the local CPU host, copy the folder to the host and have the Agent pass `argv` `python3 bench_task.py --task <id> --<flag> <value> --seed <n>` with `cwd` set to that copy, as `./scripts/local-cpu-loop.sh` does for its fixture. Gemcp does not enforce the bounds on raw argv, so there they are only in the brief.
2. Give the Agent the brief for one task: the question; a hypothesis titled as in the table above whose summary is the task's criterion (see `Tasks()` in `internal/researchbench/tasks.go`); the outside claim to record as an observation; the workload and bounds; the budget; and the instruction to finish with a decision titled `Decision: hypothesis supported|refuted|inconclusive`.
3. Confirm proposals as the Owner until the budget is spent.
4. Grade the Study:

```bash
GEMCP_BENCH_URL=http://127.0.0.1:8080/mcp GEMCP_BENCH_TOKEN=<agent token> \
GEMCP_BENCH_STUDY=<study id> GEMCP_BENCH_TASK=lr-sweep \
  go test ./internal/researchbench -run TestScoreLiveStudy -v
```

The scorer re-simulates each Experiment from its stored argv, so the run must have executed `bench_task.py`. CI runs only the in-process benchmark; this live path is not exercised there.

## Adding a task

Add the workload to `gemcp.yaml`, the formula to `bench_task.py`, and a `Task` to `tasks.go` with its criterion, baseline, threshold, budget, and truth. `TestWorkloadManifestMatchesTasks`, `TestSimulatorMatchesBenchTask`, and `TestTasksHaveTheAnswersTheyClaim` keep the three in step. Give the task one trap that a careless Agent falls into, and keep its formula to `+ - * /` and table lookups so Go and Python agree exactly.
