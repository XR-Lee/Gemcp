# Phase-zero AutoDL validation

Phase zero verifies provider behavior before Gemcp enables production scheduling. Read probes do not create resources. A live Elastic Job probe can incur charges and is blocked by several independent gates.

## Credential handling

Use a developer Token from the AutoDL console. Never commit it, put it in a command argument, or paste it into an issue or chat.

```bash
read -rsp 'AutoDL developer token: ' GEMCP_PHASE0_AUTODL_TOKEN
printf '\n'
export GEMCP_PHASE0_AUTODL_TOKEN
```

The default API host is `https://api.autodl.com`. An explicitly verified alternative can be set with `GEMCP_PHASE0_AUTODL_BASE_URL`; non-HTTPS URLs are rejected.

## Read-only probes

Pro account visibility:

```bash
./bin/gemcp phase0 read --backend pro > phase0-pro-read.json
```

Enterprise Elastic visibility and regional inventory:

```bash
./bin/gemcp phase0 read \
  --backend elastic \
  --region westDC2 \
  > phase0-elastic-read.json
```

Reports contain wallet amounts in AutoDL integer units (milli-CNY), image metadata, non-secret resource summaries, and Provider request IDs. They never contain the developer Token or root credentials.

## Minimal live Elastic Job

1. Copy `examples/phase0-elastic-job.json` outside the repository.
2. Replace the image UUID, GPU candidates, CUDA range, region, and price ceiling with values observed by the read probe.
3. Review the conservative estimate:

```text
ceil(price_to_milli_per_hour * gpu_num * max_runtime_seconds / 3600)
minimum 10 milli-CNY
```

4. Run with an explicit per-run cap. The program rejects any cap above 20,000 milli-CNY (CNY 20):

```bash
./bin/gemcp phase0 job \
  --spec /secure/path/phase0-elastic-job.json \
  --spend-cap-milli 100 \
  --confirm-live-spend=I_ACCEPT_AUTODL_CHARGES \
  > phase0-elastic-job-report.json
```

The probe creates one replica, writes diagnostics under `/root/autodl-fs/gemcp-phase0/<probe-id>/probe.log`, polls deployment/container/events, then attempts both stop and delete with an independent cleanup timeout. Cleanup failures remain visible in `cleanup_warnings` and require immediate manual action.

The CNY 20 limit is the aggregate phase-zero authorization, not a target. The CLI enforces a per-run ceiling but cannot know spending from probes run on another machine; the operator must keep the aggregate below CNY 20.

## Acceptance checklist

Record these facts before selecting the M0 backend:

- Enterprise eligibility and the exact API host.
- Wallet, image, inventory, deployment, container, and event response shapes.
- Provider status ordering and request IDs.
- Actual `price` units and billed wallet delta.
- Whether command exit ends billing.
- File storage mount and output survival after deployment deletion.
- Stop/delete behavior after completion and on duplicate calls.
- Representative permission, inventory, validation, timeout, and rate-limit errors.
- Cold-start time, `reuse_container` startup time, residual-data behavior, and any stopped-cache charge.

Do not implement around an undocumented behavior until this report confirms it.
