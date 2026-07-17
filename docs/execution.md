# Execution and shutdown enforcement

`v0.6.0` adds the M0 production lifecycle for AutoDL Private Cloud Jobs. New dispatch is deliberately opt-in: an upgrade does not start existing queued experiments, while reconciliation and terminal settlement for existing Attempts remain active.

## Arming checklist

Keep new dispatch disabled while deploying and migrating:

```dotenv
GEMCP_SCHEDULER_ENABLED=false
GEMCP_PUBLIC_URL=https://gemcp.example.com
```

Before changing the flag to `true`:

1. Confirm the public URL reaches this controlplane over HTTPS without an interactive access challenge.
2. Start the Compose `watchdog` and verify `GET /api/v1/runtime/status` reports a recent watchdog heartbeat.
3. Validate the Private Cloud credential from the Provider page.
4. Confirm project budgets, approved image, CUDA value, GPU names, resource bounds, and price ceiling.
5. Configure and test SMTP notifications.
6. Review every existing `queued` experiment; each becomes eligible when scheduling is armed.

Then set `GEMCP_SCHEDULER_ENABLED=true`, choose a conservative `GEMCP_GLOBAL_CONCURRENCY`, recreate the controlplane with `docker compose up -d --no-deps --force-recreate controlplane`, and recheck Overview health.

## Durable state machine

PostgreSQL is authoritative. In-memory goroutines never own queue state.

```text
queued -> provisioning -> running -> collecting | cancelling
       -> succeeded | failed | cancelled | timed_out | provider_error
```

Dispatch runs in a Serializable transaction. It rechecks project status, project and global concurrency, the active budget period, and the validated Private Cloud account before creating:

- one immutable `Attempt`;
- one encrypted, scoped Runner Token;
- one owned `ProviderResource` with a deterministic `gemcp-<attempt-uuid>` name;
- experiment and service leases;
- an audit event.

The ownership record is committed before the Provider create call. If the response is lost, Gemcp reconciles the deterministic name before taking another action. An Attempt issues at most one create request. Only after the name is confirmed absent can Gemcp fail that Attempt and schedule a new one. A truncated Provider listing is never interpreted as absence.

Known request rejections are terminal Provider errors. Ambiguous transport failures and capacity failures use bounded infrastructure Attempts. A created Job is never retried for command failure, OOM, cancellation, timeout, or missing Runner completion.

## Private source delivery

The GitHub Deploy private key never enters the experiment container.

1. The controlplane re-fetches the exact commit using the pinned GitHub host key and encrypted Deploy Key.
2. It creates a bounded tar.gz archive on private temporary storage.
3. The experiment downloads it through an Attempt-scoped Runner Bearer Token.
4. The key, known-host file, and bare repository are deleted after archive generation; the archive is deleted when streaming finishes.

The Token can access only its own specification, source, and event endpoint. Its digest is indexed for authentication. Recoverable ciphertext exists only so a restarted controlplane can finish uncertain provisioning; both forms are cleared when the Attempt retires or the experiment finalizes.

## Runner

The Provider command downloads a small Python bootstrap. `v0.6.2` fixes the incomplete one-line URL opener found during the first live control-plane trial by using Python's standard opener construction with an explicit redirect-denying handler. It then:

- safely downloads and bounds the source archive;
- reports `started` and heartbeats every 15 seconds;
- runs the immutable command in a separate process group;
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

Gemcp never automatically deletes durable output. `list_artifacts` reports Runner-managed filenames without browsing arbitrary shared-storage paths.

Approved images must provide `/bin/sh`, Python 3, and TLS root certificates.

## Deadline enforcement

Three layers protect shutdown:

1. The Runner has a local monotonic hard timer.
2. The scheduler reconciles desired state, Provider state, callbacks, and deadlines.
3. The separately deployed watchdog scans only owned resources with a stop request or elapsed hard deadline.

At the initial runtime deadline, Gemcp records one configured extension and queues a warning. At the extended deadline, Runner and scheduler request termination. The resource hard deadline includes termination grace plus a bounded heartbeat/observation margin. Timeout cleanup waits for either the Runner completion callback or that hard deadline; Watchdog then enforces stop and delete even if controlplane is unavailable.

Cancellation and emergency stop do not grant an extension. Stop and delete are idempotently retried. A resource is marked deleted only after the Provider no longer lists its deployment. Released `in_cache` containers are observations, not active owned deployments.

## Budget settlement

Submission reserves the maximum configured price across command runtime, the automatic timeout extension, termination grace, 600 seconds of provisioning uncertainty, and a 30-second shutdown-observation margin. At dispatch, Gemcp migrates that reservation into the current month if it crossed the project's configured monthly billing boundary and rechecks capacity. Historical queued records that omit extension or grace from their reservation are failed without Provider creation and must be resubmitted. Terminal settlement writes one negative release and one positive estimated charge entry. Estimates use the configured maximum price, observed resource lifetime, and bounded integer arithmetic; they are operational controls, not financial invoices.

`v0.6.0` had complete mocked lifecycle and fault-path coverage but was not allowed to create a paid Job during release validation. The first authorized live control-plane trial on `v0.6.1` confirmed create, observation, provisioning-timeout cleanup, deletion confirmation, and ledger settlement; it also exposed the incomplete Runner downloader fixed in `v0.6.2`.
