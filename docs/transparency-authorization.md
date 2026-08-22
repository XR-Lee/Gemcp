# Transparency as authorization

Status: future outlook after `alpha-0.16` / `v0.16.2`. Not scheduled and not an implementation spec.

`v0.16.2` already forces every spendable scientific step through MCP. The later product question is whether that trail stays a journal, or becomes the way Gemcp grants permission.

## Why this is now possible

An external harness can think wherever it wants. Gemcp only accepts work through bounded tools:

```text
get_next_actions
        │
        ▼
prepare_experiment  ── from_node_id + expected metric enter the digest
        │
        ▼
Owner confirms the exact digest
        │
        ▼
submit_prepared_experiment  ── writes the Graph run
        │
        ▼
close_run  ── the only writer of a result
```

If that is the only door, the Graph is not a notebook attached after the fact. It is the authorization log: who asked, which claim the spend was for, what the Owner confirmed, and which metric closed the run. Off-graph Experiments remain Lab debris. They can exist for diagnostics. They are not evidence.

## Transparent annotation

Annotation here is not a comment thread. It is a typed, Owner-visible label on a Graph object:

- **Claim**: a hypothesis, result, or decision in bounded plain text.
- **Provenance**: the legal edge that produced it (`leads_to`, `produced`, `supports`, `contradicts`).
- **Approval**: the confirmation digest, Actor, and time the Owner accepted.
- **Evidence**: the same-Project Experiment bound to the run, plus the metric `close_run` recorded.
- **Standing**: whether that object may authorize the next spend.

Today most of this is implicit. The Owner sees a next action and a lineage canvas. Later, those fields can be first-class: “Owner confirmed this digest”, “this result supports H1”, “this run is orphaned and cannot be cited”.

The point of making them explicit is not prettier history. It is so a later Agent, a collaborator, or Gemcp itself can answer a single question: **is this step authorized, and by which annotated claim?**

## Authorization as the next legal edge

Current authorization is coarse. A Token has `read` / `submit` / `cancel` / `configure`, and paid work still needs a human digest. That is the right safety floor. It is not a research permission model.

The intended refinement:

| Today | Later |
| --- | --- |
| Scope says the Agent may submit | The Graph says *which* node it may spend from |
| Digest approval is a one-off financial gate | The same approval annotates a claim and unlocks only its legal successors |
| `close_run` writes a result | A closed, on-graph result is the only object later work may cite |
| Orphans are a console badge | Orphans are non-authorizing: they cannot support a hypothesis or a standing policy |

This is closer to a capability than to a role. The Agent is not authorized to “do science”. It is authorized to close *this* run, or to prepare the next experiment from *this* plan, because the Owner already annotated that path.

A standing policy, if it ever exists, should be keyed the same way: a Graph path, an argv class, an expected metric, and a digest family. The presence of `submit` remains insufficient. A free-text chat approval remains insufficient.

## What a later reader should be able to verify

A person who was not at the keyboard should be able to export a Study snapshot and see, without GPU IDs or Provider consoles:

```text
Question
  └─ Hypothesis H1          Owner-visible claim
       └─ Plan              next legal spend
            └─ Run          digest D, commit, argv
                 └─ Result  metric M, close_run only
                      └─ Decision  supports / contradicts H1
```

That snapshot is the transparency artifact. If Gemcp later signs it, the signature attests the control plane's record, not the scientific truth of the claim. The lab still owns interpretation. The platform owns whether the work was in-contract.

Possible later readers: the same Owner next month, a second Agent on a new Token, a collaborator who should not receive Node credentials, or a paper supplement that cites a Study ID instead of a folder of logs.

## What this is not

- Not a public science network, marketplace, or social graph.
- Not an in-process harness. LangGraph and coding Agents stay outside; MCP remains the constraint.
- Not automatic paper writing, peer review, or a claim that a closed run is correct.
- Not a replacement for Token scopes, digest confirmation, Docker isolation, or budget caps. Those stay. The Graph specializes them.

## Later increments

These are outlook items, not a committed sequence.

1. **Explicit Owner annotations** on nodes and edges: confirm, supersede, or reject a claim without starting a workload.
2. **Citation rule**: only `close_run` results that are on-graph may `support` or `contradict` a hypothesis.
3. **Signed Study export**: a bounded, credential-free snapshot an Owner can attach to notes or a paper.
4. **Standing policies keyed by Graph path**: auto-approve a digest class only when `from_node_id`, argv family, and expected metric already match an annotated plan.
5. **MCP Resource for a frozen Study**: `gemcp://research/{study}` as the single readable contract a later Agent must load before it may spend.
6. **Attestation language in the Owner console**: show “authorized evidence” versus “Lab-only / off-graph” as a permission state, not only a badge.

Until then, `alpha-0.16` already behaves as the first half of this model: if it did not go through MCP and the Graph, Gemcp does not treat it as research.
