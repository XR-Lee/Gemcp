# Research workbench

Status: released as `v0.16.2` / `alpha-0.16`

UI kits in `v0.16.1`: Vue Flow for the Graph, Reka UI for selectors and dialogs, VueUse for live refresh, Motion for enter transitions.

`v0.16.2` turns that Graph into an MCP execution contract: legal edges, `get_next_actions`, `from_node_id` in the Proposal digest, automatic run binding on submit, and `close_run` as the only result writer.

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
  └── Study
        ├── IterationPlan (current plus superseded history)
        ├── ResearchNode (question, hypothesis, plan, run, result, observation, decision)
        └── ResearchEdge (leads_to, compares, supersedes, supports, contradicts, produced)
```

Constraints:

- At most 32 Studies per Project.
- At most 128 nodes and 256 edges per Study.
- At most 16 plan steps.
- Titles and summaries are bounded plain text. Prompts, chain-of-thought, credentials, and environment dumps are rejected.
- Linking an Experiment only accepts an Experiment from the same Project.
- Updating a plan supersedes the previous active plan instead of mutating history.
- Graph writes are Project-scoped and audited.

## Agent contract

The Graph is the execution contract. External harnesses stay outside Gemcp; MCP tools constrain what they may record and spend.

- `get_research_workspace` (`read`): return Studies, the selected plan, Graph, and legal next actions.
- `get_next_actions` (`read`): return only Graph-legal next steps.
- `update_research_workspace` (`submit`): create or update a Study, replace the active plan, or record a Graph node and optional legal edge.
- `prepare_experiment` (`submit`): when a Study exists, `from_node_id` must be a hypothesis or plan node and is bound into the confirmation digest.
- `submit_prepared_experiment` (`submit`): creates the Experiment and writes the `run` node.
- `close_run` (`submit`): the only way to write a `result` on that run after the Experiment is terminal.

`produced` is legal only from `run` to `result`. Experiments that are not bound to a Graph node are marked orphaned on the Evidence page.

## Owner review

The research home shows:

- the active Study question
- the current next action
- the Graph
- the latest linked result

UUIDs, digests, backend IDs, and reservation math remain in Experiment detail and the Lab layer.

## Outlook

If MCP remains the only door, the same trail can later authorize work, not only record it. A confirmed digest, a closed on-graph result, and an Owner annotation would be capabilities: they unlock the next legal edge. Off-graph runs would stay non-authorizing. That is written as a future outlook in [Transparency as authorization](transparency-authorization.md), not as a committed release.
