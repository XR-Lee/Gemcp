# Hypothesis–experiment Graph contract

Status: current MCP and Owner UI contract. Shared vocabulary for Agents, CLI clients, and the Owner console.

This is the single spec. MCP tools, Owner screens, and Agent guides use these words. Do not invent a second vocabulary for the same objects.

## Objects

| Word | Meaning |
| --- | --- |
| **Study** | One research question inside a Project. Plans, nodes, and edges belong to it. |
| **Question** | The Study's opening Graph node. It cannot start a paid run. |
| **Hypothesis** | A testable claim. New Experiments hang off a connected hypothesis, or a plan under that hypothesis. |
| **Plan** | Remaining work under a Study. A plan may prepare only when it traces back to a hypothesis through `leads_to` parents in an active Study. |
| **Experiment** | The Lab job: argv, image, GPU, logs, reservation. Created only after a Graph origin is bound into the proposal digest. |
| **Run** | The Graph node written when a prepared Experiment is submitted. It links the Experiment to its hypothesis or plan. |
| **Result** | The terminal scientific write for a run. Only `close_run` may create it (`run -produced-> result`). |
| **Highlight observation** | The claim `close_run` hangs on the originating hypothesis (`hypothesis -leads_to-> observation`). It carries `observation -supports\|contradicts-> hypothesis` only when `close_run` records that verdict. |
| **Verdict** | What a closed run's metric means for its hypothesis: `supports`, `contradicts`, or `inconclusive` (the default). It is a judgment of the measured number, never the Experiment's exit state. A failed run is always inconclusive. |
| **Decision** | An Owner-visible judgment after evidence, before the next Experiment. |
| **Orphan** | An Experiment that never entered the Graph. A Lab badge for legacy or bad data. Not a way to create new work. |

## Happy path

```text
update_research_workspace   record a connected hypothesis (from the question)
        │
        ▼
get_next_actions            suggest the next decision or Experiment from that hypothesis,
                            its runs, and its observations
        │
        ▼
prepare_experiment          from_node_id = that hypothesis, or a plan under the Study
        │
        ▼
Owner confirms the digest
        │
        ▼
submit_prepared_experiment  validates the bind before spending, then writes the run
        │
        ▼
get_experiment              poll state, assessment, attempts, log_tail, metrics
        │
        ▼
close_run                   writes the result, a highlight observation on the hypothesis,
                            and the verdict on what the metric means
        │
        ▼
export_research_plan_sync   dry-run a docs-only markdown amendment for the protocol branch
```

A local CPU or CLI Agent follows that path. It does not create an Experiment first and attach the Graph later.

## Spend rules

1. **The origin must trace back to a hypothesis.** `prepare_experiment` needs `from_node_id` on a connected hypothesis, or on a plan that reaches a hypothesis through `leads_to` parents (plans, questions, and decisions are traversed). Isolated nodes, and plans hanging only off the question, cannot spend — `close_run` could never link their highlight.
2. **Money is only committed to a bindable run.** `submit_prepared_experiment` re-validates the Graph origin *before* creating the Experiment and reserving budget; a doomed bind rejects the submission with nothing spent. If the Graph changes in the instant between that check and the bind, the response still carries the submitted Experiment plus `graph_bind_warning` — do not prepare again; fix the Graph and retry `submit_prepared_experiment` to bind the same Experiment.
3. **A full Study cannot take another run.** A Study holds at most 128 nodes, and each closed run adds three (run, result, highlight). Once the Study is full, `prepare_experiment` refuses the origin with `research graph node limit reached`, so the Owner is never shown a digest that cannot be submitted. `submit_prepared_experiment` checks again before spending, because the Graph can fill between prepare and submit.
4. **A proposal prepared before the Study existed goes stale once a Study opens.** Submit rejects it; prepare again with `from_node_id` on the Graph.
5. **Advanced `submit_experiment` cannot skip the Study.** If an active Study exists, that tool is rejected (idempotent retries of a submission accepted before the Study still return the same Experiment). Use `prepare_experiment` with `from_node_id`.
6. **Orphans are a badge.** Evidence still shows Off-graph for leftover or failed binds. New scientific work does not start that way.

## `get_next_actions`

This tool is the next scientific step, not a cap-8 list of every legal edge.

It reads the selected Study and, for each hypothesis, looks at existing runs, results, and observations. Evidence attribution stops at hypothesis boundaries: a run under a follow-up hypothesis belongs to that hypothesis only, never to its ancestors.

- no hypothesis → record one from the question
- hypothesis with no run → prepare an Experiment from that hypothesis
- open run → wait (`get_experiment`) or `close_run` when terminal
- closed evidence and no decision → record a decision; the detail repeats the recorded verdict, and a failed run is described as no evidence either way
- closed evidence with a decision → prepare the next Experiment from that hypothesis, until the decision spawns a follow-up hypothesis, which then owns its own next step
- a result or decision is on the Graph → also offer `export_research_plan_sync` (docs-only; never a training run)

When the Study has a route, prepare actions include `allowed_ref_pattern`, `protocol_branch`, and `protocol_doc_path`. Pass a matching live code `ref`. With `code_ref_pattern` set, omitting `ref` is refused (it would otherwise bind the repository default branch).

Import-time mapping of historical branches still uses `update_research_workspace`. `get_next_actions` is not a stop signal for that reconstruction, and it is not a license to spend before the Owner confirms the digest.

## Route binding

A Study may declare a **route**:

| Field | Meaning |
| --- | --- |
| `protocol_branch` | Docs-only protocol ref, typically `research-plan`. |
| `protocol_doc_path` | Chapter, card id, or markdown path on that branch. Must not point at a frozen recipe (`gemcp.yaml`, `Dockerfile`, `requirements*`, `recipes/`). |
| `code_ref_pattern` | Live experiment ref family, typically `autoresearch/*` (one segment) or `autoresearch/**` (nested). |

Hypotheses inherit the Study route. Recording a Graph node still never starts a workload. `prepare_experiment` on a Graph-linked Study that has a repository **and** a `code_ref_pattern` refuses an omitted `ref` or a ref outside the pattern. Host-process / local CPU prepares (no repository) skip that check. The three remote backends still do not fall back to one another.

## Git identity

A prepared or submitted Experiment records `git_identity`: repository, `requested_ref`, commit SHA when known, and the repository default branch as **informational only**. Owner cards, hypothesis records, and Graph node-detail show `requested_ref` when one exists. They do not invent the repository default branch as a live experiment ref. Host-process Cloud SSH / local CPU loops keep the stored `host` sentinel off public git tokens; `close_run` treats that placeholder as absent evidence.

## Plan sync (Graph → protocol docs)

`export_research_plan_sync` (MCP) and `POST /projects/:id/research/plan-sync` (Owner) export a markdown amendment aimed at the protocol branch. Dry-run is the default (`read`). `dry_run=false` writes an audit receipt (`submit`) and still does not push git. Gemcp never force-pushes and never writes frozen recipe files. Markdown is not the source of truth for metrics.

## Operator flow (external scientific repo)

Example: bind `XR-Lee/DynamicPointMamba` (or any similar lab repo) without dumping training code onto `research-plan`.

1. Register the repository (`register_repository` / Owner import). Public HTTPS activates immediately. Private repos stay `pending_key` until verify (Deploy Key or write-only `https_token`); Graph and catalog observation writes remain possible while pending.
2. Create or update the Study with `repository_id` plus route: `protocol_branch=research-plan`, `protocol_doc_path` for the chapter/card, `code_ref_pattern=autoresearch/*`.
3. Record a connected hypothesis. Call `get_next_actions`.
4. `prepare_experiment` with `from_node_id` on that hypothesis and `ref` on an `autoresearch/…` branch. Owner confirms the digest.
5. `submit_prepared_experiment` writes the Graph `run`. Poll `get_experiment`. `close_run` writes the result and highlight.
6. `export_research_plan_sync` (dry-run first). Apply the markdown on the docs-only protocol branch. Do not merge training code into `research-plan`.

## `close_run`

`close_run` is the only writer of a result. It also writes a highlight observation linked to the hypothesis the run hangs off (`hypothesis -leads_to-> observation`) and to its concrete result (`result -leads_to-> observation`), so the Owner view shows the right highlight when one hypothesis accumulates several runs.

`verdict` records what the metric means for that hypothesis. `supports` writes `observation -supports-> hypothesis` and `contradicts` writes `observation -contradicts-> hypothesis`. `inconclusive`, the default, writes neither. Only a succeeded run that recorded a metric may support or contradict; a crash, timeout, cancellation, or a run closed as `failed` is inconclusive because it says nothing about the claim. `status` still means whether the run produced valid evidence, not whether the hypothesis held. Graphs closed before this rule carry `supports`/`contradicts` edges that were derived from the Experiment state; re-read their metrics before citing them.

Because every new spend already required a hypothesis ancestor, that link exists on the happy path. A legacy run bound before this contract still closes: the result is written, the highlight is skipped, and the response carries a warning telling the agent to reconnect the origin.

`close_run` terminal writes are exempt from the Study node and edge caps — a full Graph must never leave a funded run permanently uncloseable. New spending on a full Study is refused at `prepare_experiment` instead, and again at `submit_prepared_experiment` if the Graph filled after prepare (spend rule 3).

Copy the scalar from `get_experiment`, pass only `metric_name` to have Gemcp copy that value from the Experiment, or omit `metric_name` to copy the prepared `expected_metric`. Optional `highlight` sets the observation title; omit it to reuse the result title. Optional `result_commit_sha` stamps the durable Git manifest. Host-process Cloud SSH experiments (including the local CPU loop) have no Git checkout: omit `result_commit_sha` and do not invent a SHA. `close_run` treats the stored host-process placeholder as absent evidence. When `experiment_id` already identifies a bound Graph run, `study_id` may be omitted even if the Project has several Studies. `get_experiment` returns that `study_id` on a Graph-linked run.

## Owner view

The Owner hypothesis list (same objects as MCP) shows, for each hypothesis:

- linked Experiments (the Graph runs)
- requested git ref (`requested_ref` from the latest bound run; omitted when none was recorded — never the repository default branch as a fallback)
- commit
- run records: state, result title, metric (marked measured when it matches the Experiment's `metrics.json`, asserted otherwise), verdict, and the highlight observation when its title differs from the result
- a tally of supports, contradicts, and inconclusive runs

The Study heading shows the route when one is set. **Export plan sync** dry-runs the same docs-only amendment as MCP `export_research_plan_sync`.

The Study "Latest result" card is the newest `result` by evidence time (`occurred_at`, then Graph write time). A later import of older evidence must not hide a newer Experiment result.

UUIDs, argv, GPU IDs, and reservation math stay on the Lab Evidence page. Double-click a Graph node with an Evidence link to open that same Experiment record; other nodes open the node-detail sidebar. That sidebar lists Branch only when the node has a `requested_ref`. A commit-only record still shows Commit.

A terminal Graph-linked Experiment exposes **Close run** on Evidence detail (and on a `close_run` next action). That Owner path calls the same writer as MCP `close_run`: result, highlight observation, and verdict. The verdict defaults to inconclusive and is offered only for a succeeded run with its expected metric.

Graph stamps keep the two questions apart: run and result nodes show whether the Experiment ran (**Ran** / **Run failed**); observation nodes show the verdict (**Supports** / **Contradicts** / **Inconclusive**). Off-graph Experiments stay a Lab badge and cannot be closed from the console.

## Legal edges

Typed grammar:

- `question -leads_to-> hypothesis | plan`
- `hypothesis -leads_to-> plan | run | decision | observation`
- `hypothesis -compares-> hypothesis`
- `plan -leads_to-> run | plan`
- `run -produced-> result`
- `result -leads_to-> decision | hypothesis | observation`
- `observation -leads_to-> decision | hypothesis`
- `result | observation -supports | contradicts-> hypothesis`

`produced` is legal only from `run` to `result`, and only `close_run` may write that result.

## Evidence nodes are read-only

Nodes Gemcp writes from Experiment evidence — the `run` bound at submission, the `result`, and the `close_run` highlight observation — are marked `system_written`. `update_research_workspace` (Agent or Owner) cannot change their title, summary, status, metric, time, or commit; it may still attach an edge to them. Record a new observation or decision to add interpretation. Any run bound to an Experiment and every result are protected the same way, including on older Graphs.

Only `close_run` marks Experiment evidence as `supports` or `contradicts`, because only it checks that the run succeeded and carries its metric. A `supports` or `contradicts` edge from a result or highlight added through `update_research_workspace` is rejected. To record your own reading, write an observation from the result and link that observation to the hypothesis; it stays the Agent's claim, not Experiment evidence.

Updating any other node by `id` is partial: omitted fields keep their stored values, so attaching an edge never erases a summary or resets a status.

Every scalar on the Graph carries `metric_source`: `experiment` when it matches the bound Experiment's `metrics.json` (directly, through the run that produced a result, or through that result's highlight), `asserted` when it was typed into the Graph. The mark is recomputed on every read.

## What this is not

- Not a product rewrite of execution, Docker isolation, or digest confirmation.
- Not a second set of names for Study / hypothesis / run / result / observation.
- Not permission to treat off-graph Experiments as new research work.
- Not a git write path: plan sync exports a patch; the Agent or Owner applies it on the docs-only protocol branch.
