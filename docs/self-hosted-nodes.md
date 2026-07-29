# Self-hosted GPU nodes

## Status

This document records the architecture for adding trusted, single-GPU PCs to Gemcp. Enrollment, one-node Docker execution, two-node independent scheduling, runtime configuration, cancellation, deadlines, and bounded result projection are implemented behind a feature flag. Existing AutoDL execution remains unchanged. Build Sessions and immutable asset publishing remain future work.

## Boundaries

MCP remains the Agent-facing control-plane boundary. A GPU PC does not expose MCP, SSH, a database, or an inbound control port. It runs a `gemcp-node` daemon that initiates outbound HTTPS connections to Gemcp.

```text
Agent --HTTPS MCP--> Gemcp control plane --PostgreSQL
                            ^
                            | outbound HTTPS
                            |
                       gemcp-node --Docker--> NVIDIA GPU
```

Gemcp owns desired state, authorization, scheduling, Experiment and Attempt records, Project policy, and asset metadata. `gemcp-node`, bbolt, Docker, and managed files own observed local state. Commands and events use at-least-once delivery with immutable IDs and idempotent processing.

## Initial platform

- Linux x86_64 with systemd.
- One NVIDIA GPU per node.
- Docker Engine and NVIDIA Container Toolkit installed by the Owner.
- A static Go `gemcp-node` binary installed as a dedicated system user.
- One Owner-configured managed storage root per node.
- No Windows, macOS, ARM, AMD, Podman, MIG, GPU sharing, or cross-node training in the first release.

## Enrollment and identity

The Owner creates a short-lived, single-use enrollment link. The node claims it with an installation ID and a bounded hardware report. Gemcp returns a provisional node-scoped Bearer Token and a pairing code. The Owner verifies the pairing code and hardware before activation.

Enrollment codes and Node Tokens are stored by Gemcp only as domain-separated digests. Each node has an independently revocable credential. A material machine or GPU identity change moves the node to `verification_required` until the Owner approves it again.

Nodes belong to the organization. An Owner explicitly authorizes each node for one or more Projects. Experiments and assets remain Project scoped.

## Synchronization

PostgreSQL is authoritative for desired state. Node-local state is authoritative for observed containers, files, and undelivered events.

- Server-to-node Commands are durable and may be redelivered until acknowledged.
- Node-to-server Events are written to a local outbox before transmission.
- Commands and Events have immutable IDs and per-node sequences.
- Node startup reconciles bbolt, Docker labels, and local manifests before accepting work.
- A heartbeat gap of 60 seconds makes a node unavailable for new work.
- A heartbeat gap of 30 minutes marks it lost and permits a best-effort infrastructure retry.
- A strict-mode retry may use another authorized, online node when its immutable inputs are available. Trusted workspaces are intentionally bound to the approved physical Node and do not fail over. Source archives and public digest-pinned OCI images are portable in strict mode; future node-local assets require explicit placement.

Enrollment, authentication, heartbeat synchronization, durable protocol records, Assignment dispatch, and Docker execution use this foundation.

## Scheduling and execution

The Gemcp scheduler selects and reserves a concrete node in a PostgreSQL transaction. Nodes do not compete to claim unbound work. A Resource Profile binds to exactly one Provider kind, so Self-hosted work never silently falls back to paid AutoDL capacity.

One Attempt occupies one node and one GPU. The daemon checks NVIDIA compute processes before reporting availability and again before starting a container. A node with a non-Gemcp compute process is `externally_busy` and does not receive work. Project concurrency and durable FIFO remain in effect. Self-hosted usage is unmetered in CNY.

Gemcp offers two explicitly separate Self-hosted policies. Strict runtime configuration pairs a digest-pinned public OCI image with a zero-cost Resource Profile and accepted GPU model names. Trusted workspace mode is an Owner-approved permissive boundary for one Project and one physical Node: the Owner supplies only a normalized absolute host directory, while Gemcp derives the GPU model, CPU limit, memory limit, Environment, and Resource Profile from current inventory.

Trusted workspace mode allows a prepared Experiment to select a public OCI image by name, tag, or digest. The approved host directory is mounted read-write at `/gemcp/workspace`, exposed through `GEMCP_TRUSTED_WORKSPACE`; the verified repository remains `/workspace` and managed outputs remain `/outputs`. Docker resolves a tag before container creation and launches the resolved repository digest. After a successful Experiment, Gemcp records that digest as the workspace Environment default and retains up to 16 successful digests for reuse. Changing the approved directory or authorization invalidates an unsubmitted Proposal.

An Agent with explicit `configure` scope may register repository records inside its authenticated Project and declare up to 32 dataset paths below that approved root. Dataset declarations accept only normalized relative paths; they never create another bind mount or expand the Owner's host authorization. Each declaration produces a fixed variable such as `GEMCP_DATASET_SCANOBJECTNN_OBJBG=/gemcp/workspace/data/ScanObjectNN/main_split`. The active declarations are bound into the Proposal digest and Experiment Environment Snapshot. Before container creation, `gemcp-node` resolves every declared path, requires it to exist, and rejects symlink escape from the approved root. Disabling a declaration changes metadata only and never deletes host data.

The Owner may disable the workspace from the same Nodes view. Gemcp refuses revocation while that Node has an active Assignment, then clears the directory authorization and disables the generated Environment/Profile and active dataset declarations. Successful image history, disabled dataset records, and audit records are retained.

This permissive mode does not grant arbitrary Docker authority. Privileged mode, the Docker socket, Host Network, host namespaces, arbitrary devices, arbitrary additional mounts, published ports, Node credentials, and control-plane credentials remain unavailable. The Node rejects a protected host root or a workspace lexically overlapping Gemcp managed storage; Docker requires the bind source to exist when it creates the container. Strict mode remains the default and continues to reject mutable image references and host workspaces.

Node registration, Project authorization, hardware inventory, version, execution modes, and heartbeat readiness are projected automatically into `get_project_options` as `self_hosted_nodes`. This discovery does not wait for a runtime configuration. A Node without a matching approved Environment/Profile remains visible with `runtime_configuration_required`. Enabling a trusted workspace automatically creates that pair; strict mode still requires the Owner-supplied digest. The response omits credentials, machine fingerprints, GPU UUIDs, and unmanaged storage paths. An approved workspace path is intentionally visible only to the Project Agent so workloads can use the bounded mount. Physical Node selection remains server-owned except that a workspace Environment is intrinsically bound to the Node whose directory was approved.

`gemcp-node` downloads the verified commit archive with its Node Token, safely extracts it under the managed storage root, and supervises the Docker container directly. The workload container receives no Node Token, Runner Token, or Docker socket. Strict mode receives no arbitrary host path; trusted workspace mode receives only the exact additional directory approved by the Owner. Existing in-container Runner behavior remains specific to AutoDL.

Containers may run as root, but never use privileged mode, host namespaces, Host Network, arbitrary devices, unapproved bind mounts, or published ports. They use a read-only root filesystem, dropped Linux capabilities, `no-new-privileges`, bounded CPU, memory and PID settings, a private IPC namespace, controlled source/output mounts, and Docker bridge networking without egress filtering.

The daemon enforces the extended runtime deadline locally even when Gemcp is unreachable. Container state is reconciled from bbolt and Docker labels after restart. Complete output and logs stay in managed node storage; while running, Gemcp receives a 64 KiB log tail and bounded `metrics.json` every 15 seconds, followed by exit status and an opaque output reference. Container deletion is reported separately through a durable cleanup-complete Event, so terminal execution and cleanup evidence remain distinct.

Prepared Experiments use a structured argv instead of `/bin/sh -lc`. A compatible `gemcp-node` advertises `execution_modes: [shell, argv]`, validates the bounded array, and launches it as the OCI process with an explicit entrypoint. The scheduler never sends an argv workload to an older Node that lacks this capability. Existing shell-mode Experiments and fixed diagnostics remain compatible with older protocol-v1 Nodes, but Nodes must be upgraded before they can run the simplified prepared path.

Use `gemcp-node v0.13.1` or later for prepared argv execution. `v0.13.1` adds compatibility with the bounded PAX commit marker produced by `git archive` while preserving path, link, type, entry-count, and payload limits.

Trusted workspace execution requires `gemcp-node v0.15.0` or later and the advertised `workspace_modes: [trusted_rw]` capability. Older Nodes remain usable for strict digest-pinned execution and report `workspace_upgrade_required` after an Owner enables the permissive policy.

Workspace dataset environment variables require `gemcp-node v0.15.1` or later and `dataset_modes: [workspace_env_v1]`. When active dataset declarations exist, an older Node reports `dataset_upgrade_required` and cannot receive that Proposal.

Existing Nodes are upgraded without re-enrollment. The Owner console generates a Node-specific coding-Agent handoff bound to the control plane's exact release and full commit. The handoff builds a verified candidate and invokes `deploy/upgrade-gemcp-node.sh`, which refuses to proceed while any managed workload container remains, preserves the credential, config, bbolt state, and storage root, atomically replaces the binary, and rolls back if the service does not remain active.

The Owner can validate this path from the [Backend diagnostics](diagnostics.md) workspace. A Self-hosted diagnostic uses a fixed built-in command but otherwise follows normal source download, Assignment, Docker, GPU, output, Event, cancellation, and cleanup behavior. It requires an online authorized Node that exactly matches the selected Resource Profile and records a zero-CNY reservation.

## Build Sessions and assets

The capabilities in this section are the next delivery stage and are not exposed by the current implementation.

Build Sessions are persistent, bounded Docker containers controlled through dedicated MCP tools. Commands are asynchronous, idempotent, audited, and individually time-bounded. A Session occupies one node slot until publish, close, idle expiry, or its hard deadline.

An Environment Snapshot is a node-local immutable Docker image produced from a Build Session. A Dataset Snapshot is a node-managed read-only directory with a complete content manifest and aggregate digest. Agent-facing specifications use logical asset IDs and controlled mount names, never host paths.

The first release does not automatically replicate assets. Missing placement is resolved by an explicit Asset Build request with a Resource Profile and optional co-location constraints. Ordinary Experiment outputs remain attached to their historical Experiment and may be mounted read-only by a later Experiment on the same node.

Large files and complete logs remain node-local. PostgreSQL stores bounded log tails, structured metrics, manifests, and locations. Online nodes can serve bounded log chunks through the authenticated control plane.

## Operations

Nodes support `active`, `draining`, and `disabled` Owner-controlled desired states. Emergency Stop affects only Gemcp-managed containers and Sessions. It never stops unrelated Docker containers, external GPU processes, Docker itself, or the host operating system.

The daemon does not silently self-update. It reports its binary and protocol versions. The Owner drains the node and runs an explicit, checksum-verified upgrade.

Temporary staging data and expired unpinned Experiment outputs are garbage-collected. Environments, Datasets, referenced outputs, and non-Gemcp Docker objects are never deleted by generic pruning.

## Delivery controls

Self-hosted dispatch defaults off. Schema and API migrations are additive. Rollout proceeds through enrollment, one-node execution, two-node scheduling, Build Sessions and assets, then the expanded Owner UI. Unit, SQLite integration, PostgreSQL migration, Docker lifecycle, restart recovery, HTTP contract, and browser tests gate each stage.
