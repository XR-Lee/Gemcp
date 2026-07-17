# Phase-zero AutoDL validation

Phase zero verifies Provider behavior before Gemcp enables production scheduling. Read probes do not create resources. Live Job probes consume compute and are blocked by several independent gates.

## Credential handling

Use a Developer Token from the matching AutoDL console. Never commit it, put it in a command argument, or paste it into an issue or chat.

```bash
read -rsp 'AutoDL developer token: ' GEMCP_PHASE0_AUTODL_TOKEN
printf '\n'
export GEMCP_PHASE0_AUTODL_TOKEN
```

Gemcp chooses the official API host from the backend:

```text
elastic or pro -> https://api.autodl.com
private        -> https://private.autodl.com
```

An explicitly verified alternative can be set with `GEMCP_PHASE0_AUTODL_BASE_URL`; non-HTTPS URLs are rejected.

## Read-only probes

Public Pro account visibility:

```bash
./bin/gemcp phase0 read --backend pro > phase0-pro-read.json
```

Public enterprise Elastic visibility and regional inventory:

```bash
./bin/gemcp phase0 read \
  --backend elastic \
  --region westDC2 \
  > phase0-elastic-read.json
```

Private Cloud image, system-image, inventory, and deployment visibility:

```bash
./bin/gemcp phase0 read --backend private > phase0-private-read.json
```

Public reports include wallet amounts in AutoDL integer units (`milli-CNY`). Private Cloud has no Developer wallet endpoint, so its report omits `wallet`. Reports contain non-secret resource summaries and Provider request IDs when the Provider returns them. They never contain the Developer Token, SSH command, or root password.

## Minimal live Job

1. Copy the matching example outside the repository or into a Git-ignored protected directory:
   - Public Elastic: `examples/phase0-elastic-job.json`
   - Private Cloud: `examples/phase0-private-job.json`
2. Replace the image UUID, GPU candidates, CUDA selector, region where applicable, and price ceiling with values observed by the read probe.
3. Review the conservative estimate:

```text
ceil(price_to_milli_per_hour * gpu_num * max_runtime_seconds / 3600)
minimum 10 milli-CNY
```

4. Run with an explicit per-run cap. The program rejects any cap above `20,000 milli-CNY` (CNY 20):

```bash
./bin/gemcp phase0 job \
  --spec /secure/path/phase0-job.json \
  --spend-cap-milli 100 \
  --confirm-live-spend=I_ACCEPT_AUTODL_CHARGES \
  > phase0-job-report.json
```

The probe creates one replica, writes diagnostics under `/root/autodl-fs/gemcp-phase0/<probe-id>/probe.log`, polls deployment/container/events, then attempts both stop and delete with an independent cleanup timeout. Cleanup failures remain visible in `cleanup_warnings` and require immediate manual action.

Private Cloud uses one `cuda_version` value such as `118` and no `region`. Public Elastic uses `cuda_from`, `cuda_to`, and `region`. Do not reuse one backend's specification for the other.

The CNY 20 limit is the aggregate phase-zero authorization, not a target. The CLI enforces a per-run ceiling but cannot know spending from probes run on another machine. Private Cloud may not expose a billable wallet, so the operator must still bound resource duration and reconcile usage through local policy.

## Acceptance checklist

Record these facts before selecting the M0 backend:

- Eligibility and exact API host.
- Wallet support, image, inventory, deployment, container, and event response shapes.
- Provider status ordering, finished counters, and request IDs.
- Actual `price` units and any available billed-wallet delta.
- Whether command exit ends resource use.
- File-storage mount and output survival after deployment deletion.
- Stop/delete behavior after completion and on duplicate calls.
- Representative permission, inventory, validation, timeout, and rate-limit errors.
- Cold-start time, `reuse_container` startup time, residual-data behavior, and any stopped-cache charge.

The selected Private Cloud observations are recorded in [AutoDL Private Cloud validation](private-cloud-validation.md). Do not implement around undocumented behavior until a live report confirms it.
