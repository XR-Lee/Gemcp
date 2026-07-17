# AutoDL Private Cloud validation

Gemcp selected AutoDL Private Cloud as the M0 execution target after a live phase-zero validation on 2026-07-17. This document records only non-secret behavior. Provider credentials, deployment UUIDs, container access data, and account identifiers remain outside Git.

## Validated API boundary

- API host: `https://private.autodl.com`.
- Developer Token authentication succeeded against image, inventory, deployment, container, and event endpoints.
- Private Cloud does not expose the public AutoDL wallet endpoint. Gemcp must enforce project budgets locally and treat provider `price` values as operational estimates, not account billing records.
- User-private images are returned by `POST /api/v1/dev/image/private/list`.
- System images used by the official console are returned by `POST /api/v2/image/list`.
- GPU stock is returned by `GET /api/v1/dev/machine/gpu_stock`. The live response is an object keyed by GPU name, unlike the array returned by the public Elastic API.
- Private deployment creation uses one `cuda_v` selector and no public Elastic region or CUDA-range fields.
- Deployment, container, event, stop, and delete endpoints otherwise follow the documented Elastic lifecycle paths.
- The observed Private Cloud responses did not include Provider request IDs.

## Minimal live Jobs

Two one-GPU Jobs used a visible CUDA 11.8 system image and a 30-second command deadline. Each run had a conservative `100 milli-CNY` local cap. The command created a probe directory under `/root/autodl-fs`, recorded the timestamp and GPU identity, and exited immediately.

Cold run:

- The container moved through `created`, `starting`, `running`, and `shutdown`.
- Cold `created -> running` time was approximately 7 seconds.
- The complete create, observe, stop, and delete operation took approximately 20 seconds.

Stopped-container reuse run:

- `reuse_container=true` reused the first cached container lineage.
- Reused `created -> running` time was approximately 4 seconds.
- The complete operation took approximately 14 seconds.

Both deployments were deleted successfully. A final read found zero deployments and the same number of idle GPUs as before the probes.

## Provider semantics

- A completed Job may report deployment `status=running` while `finished_num=1`. Reconciliation must treat the finished count as terminal instead of waiting only for the status string.
- Deleting a deployment can leave its stopped container in Provider state `in_cache`. The cache did not reserve a GPU and was subsequently reused, but it is disposable Provider state and must never be authoritative.
- The observed container price was `1000 milli-CNY/hour`, below the probe's `9000 milli-CNY/hour` scheduling ceiling.
- Container API responses may contain SSH and root-password data under `info`. Gemcp deliberately drops those fields during decoding and must not persist or log them.

## Remaining validation

- Confirm the probe files directly from the configured network-storage mount and verify that output survives deployment deletion independently of container cache reuse.
- Determine whether this Private Cloud installation charges for stopped `in_cache` containers; there is no developer wallet endpoint for a before/after comparison.
- Validate representative permission, no-capacity, invalid-image, timeout, duplicate stop, and duplicate delete errors.
- Confirm exact retry behavior for failed Job commands before enabling automatic infrastructure retries.

These remaining items do not change the M0 Provider choice, but production scheduling must keep conservative deadlines, idempotent reconciliation, and an independent shutdown path.
