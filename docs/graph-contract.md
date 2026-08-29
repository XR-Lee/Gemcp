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
get_experiment              poll state, log_tail, metrics
        │
        ▼
close_run                   writes the result and a highlight observation on the hypothesis
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

Import-time mapping of historical branches still uses `update_research_workspace`. `get_next_actions` is not a stop signal for that reconstruction, and it is not a license to spend before the Owner confirms the digest.

## `close_run`

`close_run` is the only writer of a result. Success also writes a highlight observation linked to the hypothesis the run hangs off (`hypothesis -leads_to-> observation`, `observation -supports|contradicts-> hypothesis`) and to its concrete result (`result -leads_to-> observation`), so the Owner view shows the right highlight when one hypothesis accumulates several runs.

Because every new spend already required a hypothesis ancestor, that link exists on the happy path. A legacy run bound before this contract still closes: the result is written, the highlight is skipped, and the response carries a warning telling the agent to reconnect the origin.

`close_run` terminal writes are exempt from the Study node and edge caps — a full Graph must never leave a funded run permanently uncloseable. New spending on a full Study is rejected at prepare/submit instead.

Copy the scalar from `get_experiment`, or omit `metric_name` to copy the prepared `expected_metric`. Optional `highlight` sets the observation title; omit it to reuse the result title. Optional `result_commit_sha` stamps the durable Git manifest.

## Owner view

The Owner hypothesis list (same objects as MCP) shows, for each hypothesis:

- linked Experiments (the Graph runs)
- git branch (`requested_ref`, else the Study repository default branch)
- commit
- run records: state, result title, highlight observation

UUIDs, argv, GPU IDs, and reservation math stay on the Lab Evidence page. Double-click a Graph node with an Evidence link to open that same Experiment record; other nodes open the node-detail sidebar.

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
