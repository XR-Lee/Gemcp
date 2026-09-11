# Execution and shutdown enforcement

Gemcp runs the same durable production lifecycle for AutoDL Private Cloud Jobs (`autodl_private`) and AutoDL Public Elastic Jobs (`autodl_elastic`). Public Pro remains phase-zero/read-only. New dispatch is deliberately opt-in: an upgrade does not start existing queued experiments, while reconciliation and terminal settlement for existing Attempts remain active. Experimental Cloud SSH is a third, non-fallback backend: the control plane opens outbound SSH, starts the Agent's argv as a host process, and observes logs and exit status. It does not use AutoDL Runner callbacks. See [Cloud SSH nodes](ssh-cloud-nodes.md).

## Arming checklist

Keep new dispatch disabled while deploying and migrating:

```dotenv
GEMCP_SCHEDULER_ENABLED=false
GEMCP_PUBLIC_URL=https://gemcp.example.com
GEMCP_SSH_CLOUD_ENABLED=false
```

Before changing the flag to `true`:

1. Confirm the public URL reaches this controlplane. AutoDL requires HTTPS without an interactive access challenge. Cloud SSH can use loopback HTTP because it does not accept inbound Runner callbacks.
2. Start the Compose `watchdog` and verify `GET /api/v1/runtime/status` reports a recent watchdog heartbeat.
3. Validate the configured Private Cloud or Public Elastic credential from the Provider page. Public Elastic requires an enterprise-verified AutoDL account.
4. Confirm that the approved Environment and Resource Profile use the same backend, then review project budgets, image UUID, region where applicable, CUDA selector or range, GPU names, resource bounds, and price ceiling.
5. Configure and test SMTP notifications.
6. Review every existing `queued` experiment; each becomes eligible when scheduling is armed.

Then set `GEMCP_SCHEDULER_ENABLED=true`, choose a conservative `GEMCP_GLOBAL_CONCURRENCY`, recreate the controlplane with `docker compose up -d --no-deps --force-recreate controlplane`, and recheck Overview health.

## Durable state machine

PostgreSQL is authoritative. In-memory goroutines never own queue state.

```text
queued -> provisioning -> running -> collecting | cancelling
       -> succeeded | failed | cancelled | timed_out | provider_error
```

Dispatch runs in a Serializable transaction. It rechecks project status, project and global concurrency, the active budget period, and the validated AutoDL account matching the Experiment backend before creating:

- one immutable `Attempt`;
- one encrypted, scoped Runner Token;
- one owned `ProviderResource` with a deterministic `gemcp-<attempt-uuid>` name;
- experiment and service leases;
- an audit event.

The ownership record is committed before the Provider create call. If the response is lost, Gemcp reconciles the deterministic name before taking another action. An Attempt issues at most one create request. Only after the name is confirmed absent can Gemcp fail that Attempt and schedule a new one. A truncated Provider listing is never interpreted as absence.

The create payload is backend-specific. Private Cloud uses the sentinel region `private` and one validated `cuda_v` value represented by equal `cuda_from` and `cuda_to` profile values. Public Elastic sends the Resource Profile region in `container_template.dc_list` and preserves the configured CUDA range. Public GPU stock is checked for that region before dispatch, but an idle-card count does not guarantee that a multi-GPU request can be placed on one machine; the Provider create response remains authoritative.

Known request rejections are terminal Provider errors. Ambiguous transport failures and capacity failures use bounded infrastructure Attempts. A created Job is never retried for command failure, OOM, cancellation, timeout, or missing Runner completion.

## Private source delivery

The GitHub Deploy private key never enters the experiment container.

1. The controlplane re-fetches the exact commit using the pinned GitHub host key and encrypted Deploy Key.
2. It creates a bounded tar.gz archive on private temporary storage.
3. The experiment downloads it through an Attempt-scoped Runner Bearer Token. An interrupted response body is discarded and the complete archive is requested again, up to the existing three-download Attempt limit.
4. The key, known-host file, and bare repository are deleted after archive generation; the archive is deleted when streaming finishes.

The Token can access only its own specification, source, and event endpoint. Its digest is indexed for authentication. Recoverable ciphertext exists only so a restarted controlplane can finish uncertain provisioning; both forms are cleared when the Attempt retires or the experiment finalizes.

## Runner

The Provider command downloads a small Python bootstrap. `v0.6.2` fixes the incomplete one-line URL opener found during the first live control-plane trial by using Python's standard opener construction with an explicit redirect-denying handler. Live diagnostics later showed that Private Cloud expands command variables and quotes before its final `bash -c` process. `v0.6.4` therefore emits a quote-free command with no shell variables and streams a Base64-encoded downloader through `/usr/bin/base64` to Python standard input. `v0.7.1` explicitly sends `User-Agent: Gemcp-Runner/1` on the initial download and every callback after Cloudflare error 1010 rejected Python's default `Python-urllib/*` signature. `v0.11.1` allows up to five minutes for both approved runtime prerequisites, retries transient initial downloads only before the Runner is executed, and writes credential-free stage markers to `gemcp-launch.log`. `v0.11.2` additionally retries interrupted source bodies from a clean temporary file and retries a transient first `started` callback within the remaining provisioning window. HTTP errors and redirects are not retried, and these pre-execution retries never launch the workload more than once. The Runner then:

- safely downloads and bounds the source archive;
- reports `started` and heartbeats every 15 seconds;
- runs the immutable execution specification in a separate process group: prepared `argv` is passed directly to `subprocess.Popen` without a shell, while Advanced and fixed diagnostic commands retain the compatibility shell mode;
- removes Runner credentials from the user process environment;
- enforces runtime plus the configured extension locally;
- sends TERM, waits the grace period, then sends KILL;
- writes `run.log` and `gemcp-result.json`;
- reads an optional bounded `metrics.json` object;
- uploads a 64 KiB UTF-8 log tail, exit code, reason, and bounded metrics.

The fixed durable path is:

```text
/root/autodl-fs/projects/<project-uuid>/experiments/<experiment-uuid>/
```

Gemcp never automatically deletes durable output. `list_artifacts` reports Runner-managed filenames without browsing arbitrary shared-storage paths. A dispatched AutoDL Attempt registers `gemcp-launch.log`; if provisioning expires before the first callback, its last marker distinguishes Bootstrap download and Runner entry when the Provider executed enough of the launch command to write the file. Bootstrap also reports a fixed, credential-free stage vocabulary to the immutable audit log. `get_experiment` and the Owner detail view expose the current Attempt ID, source-download count, latest stage, stage time, and bounded exception type. Arbitrary error text is never accepted. A terminal Bootstrap failure that reaches the controlplane records `runner_bootstrap_failed` and immediately requests managed Provider cleanup.

### Runtime observability

The Runner `started` callback reports a validated runtime observation containing only the actual working directory, output directory, bounded `CUDA_VISIBLE_DEVICES` value, and bounded GPU index, UUID, and name records from `nvidia-smi`. It never reports the general process environment. Heartbeats project a 64 KiB UTF-8 log tail and a bounded `metrics.json` object onto the current Attempt and Experiment for the Owner Console.

Self-hosted Nodes report the fixed container paths and assigned GPU binding only after `workload_started`. A pending Node Command is not presented as an observed runtime. While running, the Node samples Docker logs and managed `metrics.json` every 15 seconds and sends them through the durable Event outbox. Complete logs remain on the Node-managed output path. After the workload result is durably recorded, successful container removal produces a separate `workload_cleanup_complete` Event; the Owner Console does not infer cleanup merely from a terminal Assignment.

Cloud SSH observation is push-mode: the control plane holds the encrypted SSH credential and polls the host process, a 64 KiB log tail, and optional `outputs/metrics.json`. It projects those onto the Assignment, Attempt, and Experiment, then deletes only `/var/tmp/gemcp/<assignment-id>/`. Agents monitor through `get_experiment`. They never receive SSH material or a remote filesystem.

The Owner Experiment workspace distinguishes immutable request data from runtime evidence. It displays repository/ref/commit, ordered argv, Environment image, Resource Profile and requested GPU beside the reported working/output paths, CUDA visibility, observed GPU devices, current backend state, cleanup evidence, live output, Attempts, and an ordered audit timeline. The Project operations feed displays controlled Agent phases and prepared Proposal state. Agent activity accepts a fixed vocabulary and bounded identifiers only; prompts, private reasoning, source contents, arbitrary free text, environment variables, and credentials are outside the protocol.

Approved images must provide the AutoDL `/root/miniconda3/bin/python3` link, `/usr/bin/base64`, and TLS root certificates.

## Prepared experiments

The normal Agent path separates zero-cost preparation from paid submission. Owners can run the same prepare from Evidence without an Agent Token. `prepare_experiment` resolves a Project-scoped repository and moving ref to a full commit SHA, validates an ordered argv or a named `gemcp.yaml` workload, selects unambiguous compatible defaults, checks source, runtime, capacity, image, and budget readiness, and stores a two-hour immutable proposal. `runtime_preset=provision` is the Gemcp-owned AutoDL dataset fetch: omit argv, confirm the digest, and the Runner downloads registered HTTPS sources onto `/root/autodl-fs`. Optional `install_dependencies` installs `requirements.gemcp.txt` or `requirements.txt` with `python -m pip install --user`. For experimental Cloud SSH it also binds the registered host, pins host, user, cwd, and argv, and injects registered `GEMCP_DATASET_*` values; Cloud SSH does not download datasets. It creates no Experiment, Attempt, Provider resource, or budget entry.

After the human confirms the exact digest, `submit_prepared_experiment` reruns preflight and rechecks all execution-relevant configuration and budget inside the Serializable creation boundary. The proposal itself is the server-owned idempotency key: one proposal can create at most one Experiment, and an identical retry returns it.

Prepared Experiments persist `execution_mode=argv` and the exact argument array. The display command is never execution authority. AutoDL executes the array with `shell=False`. A Self-hosted Node must advertise argv capability and passes the vector directly to the OCI process using an explicit entrypoint. Cloud SSH starts the same argv vector as a host process over SSH. Existing Experiments default to `execution_mode=shell`; `submit_experiment` remains the explicit Advanced compatibility path.

## Backend diagnostics

The Owner Diagnostics workspace creates real Experiments for fixed `gpu_connectivity` and `pytorch_cuda` suites. It does not bypass source delivery, FIFO scheduling, concurrency, Runner callbacks, Node Commands, deadlines, settlement, cancellation, or managed cleanup. AutoDL diagnostics disable stopped-container reuse and require explicit confirmation of the displayed paid reservation. Self-hosted diagnostics use the selected digest-pinned OCI runtime and a zero-CNY reservation.

Preflight checks runtime health, active and queued concurrency, exact commit archive safety, current budget, backend capacity, image policy, and callback/cleanup prerequisites. A proposal digest binds confirmation to execution-relevant repository, image, resource, command, runtime, and cost fields. The service reruns preflight and compares that digest again inside the creation transaction, rejecting configuration drift. Read [Backend diagnostics](diagnostics.md) for result interpretation and API details.

## Deadline enforcement

Three layers protect shutdown:

1. The Runner has a local monotonic hard timer.
2. The scheduler reconciles desired state, Provider state, callbacks, and deadlines.
3. The separately deployed watchdog scans only owned resources with a stop request or elapsed hard deadline.

At the initial runtime deadline, Gemcp records one configured extension and queues a warning. At the extended deadline, Runner and scheduler request termination. The resource hard deadline includes termination grace plus a bounded heartbeat/observation margin. Timeout cleanup waits for either the Runner completion callback or that hard deadline; Watchdog then enforces stop and delete even if controlplane is unavailable.

Cancellation and emergency stop do not grant an extension. Stop and delete are idempotently retried. Gemcp explicitly stops before deletion even though Public Elastic may stop an active deployment as part of delete. A resource is marked deleted only after the Provider no longer lists its deployment. Released `in_cache` containers are observations, not active owned deployments.

## Budget settlement

Submission reserves the maximum configured price across command runtime, the automatic timeout extension, termination grace, 600 seconds of provisioning uncertainty, and a 30-second shutdown-observation margin. At dispatch, Gemcp migrates that reservation into the current month if it crossed the project's configured monthly billing boundary and rechecks capacity. Historical queued records that omit extension or grace from their reservation are failed without Provider creation and must be resubmitted. Terminal settlement writes one negative release and one positive estimated charge entry. Estimates use the configured maximum price, observed resource lifetime, and bounded integer arithmetic; they are operational controls, not financial invoices.

`v0.6.0` had complete mocked lifecycle and fault-path coverage but was not allowed to create a paid Job during release validation. The first authorized live control-plane trial on `v0.6.1` confirmed create, observation, provisioning-timeout cleanup, deletion confirmation, and ledger settlement; it also exposed the incomplete Runner downloader fixed in `v0.6.2`. Later `v0.6.2` and `v0.6.3` trials made no callback request. Bounded live diagnostics proved that Private Cloud preserved and executed `cmd`, that Miniconda appears after the command process starts, and that `customer.cmd.sh` expands command variables and quotes before the final `bash -c`. `v0.6.4` uses the validated quote-free encoded launch protocol and a bounded interpreter wait.
