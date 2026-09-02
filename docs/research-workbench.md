# Research workbench

Status: released as `v0.17.0` / `alpha-0.17`

UI kits in `v0.16.1`: Vue Flow for the Graph, Reka UI for selectors and dialogs, VueUse for live refresh, Motion for enter transitions.

`v0.16.2` turns that Graph into an MCP execution contract: legal edges, `get_next_actions`, `from_node_id` in the Proposal digest, automatic run binding on submit, and `close_run` as the only result writer.

The current Agent and Owner vocabulary is the single [Hypothesis–experiment Graph contract](graph-contract.md). MCP and the Owner console use the same words.

`v0.17.0` makes an imported repository usable: Study import, directory-scoped MCP, a time-axis Graph with fullscreen and double-click detail, success/failure stamps, and default selection of the latest import. Double-click the node body to open the same Experiment record as the Evidence link when the node is linked; otherwise the node-detail sidebar opens.

The Graph time axis uses each node's `occurred_at` (git committer date for historical evidence, Experiment start/create time for Gemcp runs). `created_at` remains the MCP write time and is not the axis.

Scope: Owner-facing research Graph and iteration plans, Agent-reported scientific progress, and a separate Lab layer for infrastructure. Execution, Docker isolation, Proposal confirmation, and Node protocol stay in place; the Graph now gates what an external harness may do next.

## Product correction

Gemcp currently presents a GPU operations console: Experiments, Diagnostics, Finance, Project, Agents, Nodes, Provider, and Alerts sit at the same rank. That matches how the control plane was built, but it is the opposite of how a researcher works.

Claude Science's useful idea is not a life-science feature set. It is an information hierarchy:

- The person sees a research question, a plan, and a lineage of results.
- Compute is a background capability: a sub-agent runs in an isolated environment on a machine the lab already owns.
- Provenance is attached to the result, not dumped as the primary screen.
- Infrastructure remains available, but it is a Lab layer rather than the home view.

Gemcp already has the execution half of that model: `gemcp-node` runs a bounded Docker workload, MCP Agents prepare immutable Experiments, and the control plane keeps credentials, budgets, and cleanup. What it lacks is a first-class research object. Experiments are still rows of UUIDs, commits, and reservations.

## Intended path

```text
Owner enrolls a Node and a Project once
A coding Agent runs on that machine inside the approved Docker boundary
The Agent maintains a Study, an iteration plan, and a research Graph
The Owner reviews the question, next action, and result lineage
Infrastructure stays one click away when something is blocked
```

The Owner should not need a Repository UUID, Environment ID, Resource Profile, host path, or reservation formula to understand what the lab is doing.

## Layers

### Research

Visible by default.

- **Study**: one research question inside a Project.
- **Iteration plan**: the current goal, next action, rationale, and bounded steps.
- **Graph**: hypotheses, runs, results, observations, and decisions, with typed edges.
- **Evidence**: an Experiment may be linked to a run or result node. The Graph shows the scientific claim; the Experiment detail still holds argv, image, GPU, logs, and cleanup.

### Lab

Separate navigation group.

- Diagnostics, Finance, Project, Agents, Nodes, Provider, and Alerts remain Owner tools.
- They do not disappear. They stop competing with the research question on the first screen.

### Execution

Unchanged.

- Sub-agents still execute only through prepared Experiments or the Advanced shell path.
- Docker isolation, trusted workspace, digest pinning, Proposal confirmation, and budget gates are not relaxed.
- Recording a Graph node never starts a GPU job.

## Data model

A Study belongs to one Project. Plans, nodes, and edges belong to one Study.

```text
Project
  ├── Repository
  │     └── ExperimentCatalogRow (Setting, 方法, 实现, metric, 结果, link, hash, research branch)
  └── Study
        ├── IterationPlan (current plus superseded history)
        ├── ResearchNode (question, hypothesis, plan, run, result, observation, decision; optional occurred_at + commit_sha)
        └── ResearchEdge (leads_to, compares, supersedes, supports, contradicts, produced)
```

The experiment catalog is bound to a registered repository, not to the Graph. It stores table-ready raw rows extracted from research branches so later analysis and paper tables can reuse Setting / 方法 / 实现 / metric / 结果 / link / hash without putting every run on the 128-node Graph. Catalog ingest never starts a workload.

Constraints:

- At most 32 Studies per Project.
- At most 128 nodes and 256 edges per Study.
- At most 16 plan steps.
- Titles and summaries are bounded plain text. Prompts, chain-of-thought, credentials, and environment dumps are rejected.
- Linking an Experiment only accepts an Experiment from the same Project.
- Updating a plan supersedes the previous active plan instead of mutating history.
- Graph writes are Project-scoped and audited.

## Agent contract

The Graph is the execution contract. External harnesses stay outside Gemcp; MCP tools constrain what they may record and spend. The recommended singleton companion deployment, Git result manifest, and autonomy boundary are defined in [Lightweight experiment harness](node-experiment-harness.md). Tool names and object words are defined once in [Hypothesis–experiment Graph contract](graph-contract.md).

- `get_research_workspace` (`read`): return Studies, the selected plan, Graph, hypothesis records, and next actions.
- `get_next_actions` (`read`): suggest the next decision or Experiment from the hypothesis, its runs, and its observations.
- `update_research_workspace` (`submit`): create or update a Study, replace the active plan, or record a Graph node and optional legal edge.
- `prepare_experiment` (`submit`): when a Study exists, `from_node_id` must be a connected hypothesis or a plan under that Study. Isolated nodes cannot prepare. The origin is bound into the confirmation digest.
- `submit_prepared_experiment` (`submit`): creates the Experiment and writes the `run` node. Bind failure is an error, not a silent skip.
- `close_run` (`submit`): the only way to write a `result` on that run after the Experiment is terminal. It also writes a highlight observation linked to the originating hypothesis. Monitor with `get_experiment`; do not SSH or infer metrics from logs.
- `get_experiment_catalog` (`read`): return registered repository identity plus extracted catalog rows. Never starts a workload.
- `record_experiment_catalog` (`submit`): persist raw experiment rows from research branches of a registered repository. Never starts a workload.

Calling `get_next_actions` before spending is an Agent operating requirement, not a separately persisted server precondition. The server enforces the Graph boundary at the write operations: `prepare_experiment` requires a connected hypothesis or plan `from_node_id`, submission binds the `run`, and `close_run` writes the terminal `result` plus the highlight observation.

`produced` is legal only from `run` to `result`. Advanced `submit_experiment` is rejected when an active Study exists. Experiments that never entered the Graph remain an orphaned badge on Evidence for legacy or bad data; they are not a way to create new work.

## Owner review

The research home shows:

- the active Study question
- the current next action
- each hypothesis with linked Experiments, git branch, commit, and run records
- the Graph
- the latest linked result

UUIDs, digests, backend IDs, and reservation math remain in Experiment detail and the Lab layer.

## Outlook

If MCP remains the only door, the same trail can later authorize work, not only record it. A confirmed digest, a closed on-graph result, and an Owner annotation would be capabilities: they unlock the next legal edge. Off-graph runs would stay non-authorizing. That is written as a future outlook in [Transparency as authorization](transparency-authorization.md), not as a committed release.
