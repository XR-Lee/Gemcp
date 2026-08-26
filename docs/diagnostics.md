# Backend diagnostics

Gemcp provides an Owner-only Diagnostics workspace for bounded end-to-end checks of the `autodl_elastic`, `autodl_private`, and `self_hosted` execution backends. Public Pro is not a diagnostic execution backend. Experimental Cloud SSH can store a `ssh_cloud` enum value but does not run this diagnostic suite in v1. A diagnostic is a real immutable Experiment, not a control-plane-only probe. It exercises the registered Git commit archive, scheduler, Attempt creation, backend provisioning, source transfer, container GPU access, callbacks, managed output, settlement, and cleanup path used by normal workloads.

Diagnostics never fall back between backends. Select the Environment and Resource Profile for the backend that must be tested.

## Built-in suites

The command is selected by Gemcp and cannot be edited.

- `gpu_connectivity`: validates `GEMCP_OUTPUT_DIR`, source working directory, `nvidia-smi`, exact visible GPU count against the Resource Profile, driver details, output writes, and managed metrics.
- `pytorch_cuda`: additionally imports PyTorch, checks CUDA availability and exact device count, allocates tensors on `cuda:0`, runs matrix multiplication, synchronizes the device, and records bounded framework and GPU metadata.

Both suites write:

```text
metrics.json
diagnostic-report.txt
```

The normal backend supervisor also retains its managed `run.log`, bounded log tail, exit code, Attempt state, and output reference. The suites do not receive Gemcp credentials, Provider credentials, Git Deploy Keys, Docker sockets, Node Tokens, or arbitrary host paths.

AutoDL diagnostics explicitly disable stopped-container reuse so the result covers a fresh managed deployment. Self-hosted diagnostics retain the existing digest-pinned public OCI image requirement and Node container isolation.

## Owner workflow

1. Open **Diagnostics** and select a Project, backend, suite, active repository, approved Environment, active Resource Profile, and full Git commit SHA.
2. Run preflight. No Experiment or budget reservation is created by this step.
3. Review every pass, warning, and failure plus the immutable command, image, GPU bounds, CPU and memory bounds, runtime limit, worst-case reservation, and proposal digest.
4. Explicitly confirm the proposal. AutoDL confirmation acknowledges a paid deployment and the displayed worst-case reservation. Self-hosted confirmation acknowledges dispatch to an authorized Node; its reservation is zero CNY.
5. Start the diagnostic. Gemcp reruns preflight immediately before its Serializable creation transaction.
6. Follow the run analysis, Attempt history, Runner stage, source-download count, backend observation, metrics, log tail, timeline, and cleanup state.
7. Do not retry a failed run while the assessment reports cleanup as pending.

The proposal digest binds the confirmation to the selected commit, built-in command, backend, repository endpoint and pinned host key, image, GPU and resource bounds, runtime, grace period, price bounds, and reservation. If any bound configuration changes after review, submission returns `DIAGNOSTIC_PROPOSAL_CHANGED`; the Owner must run preflight and review the replacement proposal. Submission uses a Project-scoped hashed idempotency key, so a transport retry cannot create a second Experiment.

## Preflight

Preflight performs bounded read-only checks:

- scheduler enablement and heartbeat;
- global and Project execution concurrency, with queued Experiments reported separately from active slots;
- public callback URL and AutoDL Watchdog health;
- exact commit archival, gzip/tar readability, entry count, path/link safety, and payload bounds;
- current-period Project budget and per-Experiment cap;
- AutoDL Provider credential/API reachability, matching idle GPU inventory in the selected region where applicable, and image discovery;
- Self-hosted feature enablement, digest-pinned image, recent Node heartbeat, Project authorization, idle Assignment state, and exact GPU model match.

A full Project or global concurrency limit is a warning because the diagnostic can remain queued. Unsafe source, unhealthy cleanup enforcement, missing callback configuration, unavailable matching backend capacity, invalid image policy, or insufficient budget blocks submission.

AutoDL image discovery is backend-specific and can be incomplete. Private Cloud's optional Web-console system-image endpoint may reject Developer Token authentication. Public Elastic has no dynamic Developer API endpoint for official base images, so a valid documented or console-provided base-image UUID may be absent from discovery. A missing image is therefore a warning; the documented Provider create operation remains authoritative. A create rejection is captured in the real Attempt and backend timeline.

For Public Elastic, the inventory endpoint reports individual idle cards in one region. It does not prove that multiple cards are colocated on a machine, so multi-GPU placement can still fail after a passing capacity preflight.

Preflight archives the commit to verify it before confirmation. Submission repeats preflight so stale capacity, budget, repository, and runtime observations cannot authorize a run.

## Lifecycle and attribution

A confirmed run atomically creates:

- one Owner-attributed `DiagnosticRun`;
- one normal immutable `Experiment` with nullable Agent Token attribution;
- one budget reservation entry, including a zero-value Self-hosted reservation;
- one `diagnostic.submitted` audit event.

Scheduler dispatch then creates normal Attempts and either an AutoDL `ProviderResource` plus Runner Token or a Self-hosted `NodeAssignment` plus durable Node command. Diagnostics do not bypass FIFO ordering, global or Project concurrency, retries, deadlines, cancellation, watchdog enforcement, settlement, or notification behavior.

Owner cancellation reuses the Experiment cancellation state machine. A queued cancellation immediately releases its reservation. A dispatched cancellation records durable stop intent and converges through normal Provider or Node cleanup. The cancellation audit actor is the authenticated Owner `user`, not a synthetic Agent Token.

## Reading results

A run is reported as passed only when the Experiment succeeded, `metrics.json` contains `diagnostic_passed: true`, and managed backend cleanup is complete. A successful process without a passing metric is classified as an invalid diagnostic result. A successful workload remains `cleanup_pending` until cleanup is observed.

Failure recommendations use controlled evidence from:

- latest Runner Bootstrap stage and bounded error type;
- source-download count;
- Attempt failure code and exit code;
- bounded workload metrics and log tail;
- ProviderResource or NodeAssignment state, stop reason, and last error;
- append-only Experiment audit timeline.

Typical classifications distinguish scheduler waiting, Provider/Node provisioning, source download or extraction, first `started` callback, missing PyTorch, CUDA incompatibility, command exit, Node loss, provisioning timeout, cancellation, and cleanup pending. Recommendations are diagnostic hints, not permission to bypass cleanup or paid-run approval.

## API

All endpoints require an authenticated Owner Session and normal CSRF protection for state changes:

```text
GET  /api/v1/projects/:id/diagnostics/options
POST /api/v1/projects/:id/diagnostics/preflight
GET  /api/v1/projects/:id/diagnostics
POST /api/v1/projects/:id/diagnostics
GET  /api/v1/projects/:id/diagnostics/:runID
POST /api/v1/projects/:id/diagnostics/:runID/cancel
```

The list endpoint returns bounded run summaries for polling. Use the run-specific GET endpoint for Attempt logs, metrics, the immutable preflight, backend observation, and timeline.

`DiagnosticRun` stores only the Owner identity, backend, suite, immutable preflight snapshot, keyed idempotency digest, and request fingerprint. It never stores plaintext idempotency keys or backend credentials. Complete AutoDL and Node access details remain outside the diagnostic API.

## Operational limits

- Diagnostics are Owner-only and are not exposed as Agent MCP tools.
- The suites are fixed and accept no arbitrary command or Secret names.
- AutoDL diagnostics are real paid workloads. Gemcp budget credit is not an AutoDL payment or account balance.
- Self-hosted diagnostics are unmetered in Gemcp but consume a real authorized GPU and Node execution slot.
- A passing suite validates the selected commit, image, runtime, GPU, callback, output, and cleanup path at that time. It does not certify unrelated images, repositories, Nodes, datasets, network destinations, or future Provider capacity.
- Large private datasets are outside this diagnostic path until Dataset Snapshot and asset-location support is implemented.
