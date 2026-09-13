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
| **Highlight observation** | The claim `close_run` hangs on the originating hypothesis (`hypothesis -leads_to-> observation`, then `observation -supports\|contradicts-> hypothesis`). |
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
close_run                   writes the result and a highlight observation on the hypothesis
        │
        ▼
export_research_plan_sync   dry-run a docs-only markdown amendment for the protocol branch
```

A local CPU or CLI Agent follows that path. It does not create an Experiment first and attach the Graph later.

## Spend rules

1. **The origin must trace back to a hypothesis.** `prepare_experiment` needs `from_node_id` on a connected hypothesis, or on a plan that reaches a hypothesis through `leads_to` parents (plans, questions, and decisions are traversed). Isolated nodes, and plans hanging only off the question, cannot spend — `close_run` could never link their highlight.
2. **Money is only committed to a bindable run.** `submit_prepared_experiment` re-validates the Graph origin *before* creating the Experiment and reserving budget; a doomed bind rejects the submission with nothing spent. If the Graph changes in the instant between that check and the bind, the response still carries the submitted Experiment plus `graph_bind_warning` — do not prepare again; fix the Graph and retry `submit_prepared_experiment` to bind the same Experiment.
3. **A proposal prepared before the Study existed goes stale once a Study opens.** Submit rejects it; prepare again with `from_node_id` on the Graph.
4. **Advanced `submit_experiment` cannot skip the Study.** If an active Study exists, that tool is rejected (idempotent retries of a submission accepted before the Study still return the same Experiment). Use `prepare_experiment` with `from_node_id`.
5. **Orphans are a badge.** Evidence still shows Off-graph for leftover or failed binds. New scientific work does not start that way.

## `get_next_actions`

This tool is the next scientific step, not a cap-8 list of every legal edge.

It reads the selected Study and, for each hypothesis, looks at existing runs, results, and observations. Evidence attribution stops at hypothesis boundaries: a run under a follow-up hypothesis belongs to that hypothesis only, never to its ancestors.

- no hypothesis → record one from the question
- hypothesis with no run → prepare an Experiment from that hypothesis
- open run → wait (`get_experiment`) or `close_run` when terminal
- closed evidence and no decision → record whether the evidence supports or contradicts the hypothesis
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

1. Register the repository (`register_repository` / Owner import). Public HTTPS activates immediately. Private repos stay `pending_key` until verify; Graph and catalog observation writes remain possible while pending.
2. Create or update the Study with `repository_id` plus route: `protocol_branch=research-plan`, `protocol_doc_path` for the chapter/card, `code_ref_pattern=autoresearch/*`.
3. Record a connected hypothesis. Call `get_next_actions`.
4. `prepare_experiment` with `from_node_id` on that hypothesis and `ref` on an `autoresearch/…` branch. Owner confirms the digest.
5. `submit_prepared_experiment` writes the Graph `run`. Poll `get_experiment`. `close_run` writes the result and highlight.
6. `export_research_plan_sync` (dry-run first). Apply the markdown on the docs-only protocol branch. Do not merge training code into `research-plan`.

## `close_run`

`close_run` is the only writer of a result. Success also writes a highlight observation linked to the hypothesis the run hangs off (`hypothesis -leads_to-> observation`, `observation -supports|contradicts-> hypothesis`) and to its concrete result (`result -leads_to-> observation`), so the Owner view shows the right highlight when one hypothesis accumulates several runs.

Because every new spend already required a hypothesis ancestor, that link exists on the happy path. A legacy run bound before this contract still closes: the result is written, the highlight is skipped, and the response carries a warning telling the agent to reconnect the origin.

`close_run` terminal writes are exempt from the Study node and edge caps — a full Graph must never leave a funded run permanently uncloseable. New spending on a full Study is rejected at prepare/submit instead.

Copy the scalar from `get_experiment`, or omit `metric_name` to copy the prepared `expected_metric`. Optional `highlight` sets the observation title; omit it to reuse the result title. Optional `result_commit_sha` stamps the durable Git manifest. Host-process Cloud SSH experiments (including the local CPU loop) have no Git checkout: omit `result_commit_sha` and do not invent a SHA. `close_run` treats the stored host-process placeholder as absent evidence. When `experiment_id` already identifies a bound Graph run, `study_id` may be omitted even if the Project has several Studies. `get_experiment` returns that `study_id` on a Graph-linked run.

## Owner view

The Owner hypothesis list (same objects as MCP) shows, for each hypothesis:

- linked Experiments (the Graph runs)
- requested git ref (`requested_ref` from the latest bound run; omitted when none was recorded — never the repository default branch as a fallback)
- commit
- run records: state, result title, highlight observation

The Study heading shows the route when one is set. **Export plan sync** dry-runs the same docs-only amendment as MCP `export_research_plan_sync`.

The Study "Latest result" card is the newest `result` by evidence time (`occurred_at`, then Graph write time). A later import of older evidence must not hide a newer Experiment result.

UUIDs, argv, GPU IDs, and reservation math stay on the Lab Evidence page. Double-click a Graph node with an Evidence link to open that same Experiment record; other nodes open the node-detail sidebar. That sidebar lists Branch only when the node has a `requested_ref`. A commit-only record still shows Commit.

A terminal Graph-linked Experiment exposes **Close run** on Evidence detail (and on a `close_run` next action). That Owner path calls the same writer as MCP `close_run`: result plus highlight observation. Off-graph Experiments stay a Lab badge and cannot be closed from the console.

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

## What this is not

- Not a product rewrite of execution, Docker isolation, or digest confirmation.
- Not a second set of names for Study / hypothesis / run / result / observation.
- Not permission to treat off-graph Experiments as new research work.
- Not a git write path: plan sync exports a patch; the Agent or Owner applies it on the docs-only protocol branch.
