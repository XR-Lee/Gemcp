# AutoDL Public Elastic validation

This page records the sanitized acceptance evidence for the Public Elastic backend. It contains no Developer Token, deployment UUID, container UUID, SSH material, or access URL.

## Phase-zero live Job

Validated on 2026-08-30 against the official Public Elastic API:

- Region: `westDC2`.
- Resource: one RTX 4090 GPU, 16 CPU cores, and 60 GiB memory.
- Image: one Provider-visible private image.
- Authorized spend cap: 100 milli-CNY (CNY 0.10).
- Conservative preflight estimate: 50 milli-CNY (CNY 0.05).
- Provider price: 1,980 milli-CNY per GPU-hour.
- Observed running interval: 31 seconds; the corresponding price-time estimate is about 17.05 milli-CNY. This is not an invoice or a measured wallet delta.
- Terminal probe status: `finished`; finished replica count: 1.
- Final container status: `shutdown`.
- Cleanup: stop and delete both returned successful Provider request IDs, with no cleanup or observation warning.
- The Job CLI exited successfully after cleanup.

The Provider deployment snapshot could still report `running` while its only container was already `shutdown`. Consumers must use container and event state for cleanup evidence instead of treating the deployment summary as an atomic terminal signal.

## What this proves

The phase-zero Job directly validates the external AutoDL contract used by Gemcp: enterprise Elastic access, deployment creation, regional scheduling, image selection, command execution, deployment/container/event observation, and bounded stop/delete cleanup.

It does not exercise the normal Gemcp scheduler, Watchdog, budget ledger, MCP authentication, or prepared-proposal confirmation flow. Those control-plane boundaries are covered by automated tests, including `TestIssue5PublicElasticMCPWorkflow`. The optional `TestLivePublicElasticResources` integration test exercises the production read-only Provider service with a protected Token file and never creates compute.

## Repeat the read-only Provider test

```bash
GEMCP_TEST_ELASTIC_TOKEN_FILE=/secure/path/to/token \
GEMCP_TEST_ELASTIC_REGION=westDC2 \
  go test -run '^TestLivePublicElasticResources$' -v ./internal/provider
```

The test creates only an in-memory Gemcp database. It queries wallet visibility, user-private images, regional GPU inventory, deployments, and per-deployment active/released containers through the production Provider service. It rejects any serialized result containing the Token or credential ciphertext.

For a paid repeat, use the independently gated procedure in [Phase-zero AutoDL validation](phase-zero.md). Never place the Token or raw report in the repository.
