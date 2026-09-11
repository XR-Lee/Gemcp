# Cloud SSH nodes (experimental host observer)

## Status

Cloud SSH is an experimental push-mode backend. An Agent with `operate_nodes`, or the Owner as a fallback, stores an encrypted SSH password or private key on the control plane. The control plane opens outbound SSH to a Linux host, starts the Agent's `argv` in the login environment, and projects process state, a log tail, and an optional `metrics.json` onto the Experiment.

This path is not AutoDL Elastic, not Private Cloud, and not a `gemcp-node` callback. The three backends do not fall back to one another.

```text
Agent --HTTPS MCP--> Gemcp control plane --PostgreSQL
                            |
                            | outbound SSH
                            v
                     Agent-prepared host --host process--> argv
```

Gemcp does three things in this version:

1. **Channel**: accept SSH and start the Agent's `argv` on the remote host.
2. **Observe**: process up/down, stdout/stderr tail, exit code, and `metrics.json` only if it exists under `GEMCP_OUTPUT_DIR`.
3. **Gate**: the Owner confirms a digest of host, user, cwd, and argv. Not an image. Not a dataset list.

## Boundaries

- Laboratory only. The Nodes page shows a permanent **Cloud SSH (experimental)** warning. The Provider page is unchanged.
- Credentials are write-only and encrypted with AES-256-GCM using AAD `gemcp:ssh-cloud:v1`. HTTP and MCP never return a password, PEM, or ciphertext.
- Registration uses MCP `register_ssh_cloud_node` and requires the separate `operate_nodes` scope (default off, not included in `configure`). The Nodes form remains a manual fallback and accepts a pasted `ssh -p 47174 root@host` command the same way VS Code Remote SSH does, then a password (or a following `密码:` / `Password:` line).
- `rotate_ssh_cloud_node_credential` replaces the encrypted password or key. Revoke and host-key review stay Owner-only. A tenant may register at most 20 active nodes. Registration binds the Token's Project.
- Probe only connects, pins the host-key fingerprint, records `uname`, and optionally reads `nvidia-smi`. It does not install software. Missing Docker, GPU, or conda is not a failure.
- Confirmed execution is a host process. There is no OCI image, no Docker create, and no Git archive upload. Repository is optional for Graph context and does not start the command. Optional `cwd` is an absolute remote path and defaults to the login home. Gemcp does not inspect that directory.
- `get_project_options` lists Cloud SSH nodes with `experimental: true`, a warning, readiness, `bound_to_project`, and blockers (`host_key_changed`, `node_not_active`, `runtime_configuration_required`, `node_busy`). The same snapshot appears on the Owner **Agent Readiness** panel without probing. `get_project_options.readiness` tells the Agent how heartbeats work.
- Emergency Stop and Cancel kill only the Gemcp-started process group. They do not delete the Agent's home or project files. Cleanup is limited to `/var/tmp/gemcp/<assignment>/`.
- Dispatch is gated by `GEMCP_SSH_CLOUD_ENABLED` (default false).

Loopback and link-local SSH targets are rejected unless `GEMCP_LOCAL_PROCESS_ENABLED=true`. That overlay keeps the Cloud SSH observer (probe, argv host process, log tail, `metrics.json`) but runs commands with local `sh -c` on `127.0.0.1` / `localhost` instead of outbound SSH. Use a write-only dummy password such as `local-process`. RFC1918 addresses still use real SSH when the control plane can reach them. The first successful probe pins the host-key fingerprint (`SHA256:local-process` for the overlay). A later change marks the node `host_key_changed` and blocks scheduling until the Owner reviews it.

## Scheduler and AutoDL

`GEMCP_SCHEDULER_ENABLED=true` accepts a credential-free HTTPS origin **or** loopback HTTP so a local control plane can dispatch Cloud SSH work. AutoDL dispatch still requires HTTPS `GEMCP_PUBLIC_URL` because the Runner must call back. Cloud SSH does not need an inbound callback; the control plane polls the host process, a 64 KiB log tail, and optional `${GEMCP_OUTPUT_DIR}/metrics.json` over SSH. Agents monitor through `get_experiment`. They never receive that SSH session.

## Data model

Cloud SSH uses its own tables. It does not reuse `SelfHostedNode` or `NodeAssignment`.

- `cloud_ssh_nodes`: label, host, port, user, auth method, encrypted credential, host-key fingerprint, status, inventory, created actor, last probe.
- `cloud_ssh_project_accesses`: Project access created from the authenticated Agent Token on register or first prepare.
- `cloud_ssh_assignments`: experiment lease, remote directory, host PID in `container_id`, and state. `Attempt.ProviderResourceID` is `ssh_cloud:<assignment-uuid>`.

The first prepared Project workload creates a zero-CNY `ssh_cloud` Environment and Resource Profile with sentinel image `host`. The recipe is `ssh-cloud:<node-uuid>`. The Nodes page shows who registered the host, whether a process is running, and whether probe succeeded. It does not show a locked image.

## Agent path

0. The Owner clicks **Handshake prompt** on Agents. That issues the MCP setup link and a single prompt: enable Gemcp, register this host if needed, then prepare training. The Nodes page copies the same prompt for a known host, without the one-time link.
1. Prepare the host yourself over your own SSH. Do not ask Gemcp to install Docker, conda, or a driver.
2. Call `register_ssh_cloud_node` with `operate_nodes`, the ssh command, and a write-only password or key. Probe runs in the background.
3. `get_project_options` — present the experimental warning. Fixed blockers are `host_key_changed`, `node_not_active`, `runtime_configuration_required`, and `node_busy`.
4. `prepare_experiment` with `argv` and optional `cwd`. Omit `image`. Repository is optional.
5. Show the confirmation digest (host, user, cwd, argv, no isolation). Wait for explicit Owner approval.
6. `submit_prepared_experiment` with the exact digest.
7. Poll `get_experiment` until a terminal state. Use its `assessment`, `attempts`, `log_tail`, and optional `metrics`. Then `close_run`. Do not SSH, fetch remote files, or parse logs for the result.

Do not invent SSH credentials. Do not fall back to AutoDL or Self-hosted. Emergency Stop still only kills the Gemcp-started process group.

## Remote layout

Work metadata lives under `/var/tmp/gemcp/<assignment-id>/` (`outputs` only). Gemcp may set `GEMCP_OUTPUT_DIR` to that outputs directory as a convenience. The command runs in the login environment and may ignore that variable. After a terminal state, the control plane removes only that assignment directory.

## v1 exclusions

- Installing or requiring Docker / NVIDIA toolkit / `dockerd`
- Public image pull and `@sha256` lock
- Uploading a Git archive
- Conda prefix, data-root, or lockfile hashes
- Scheduling only when a GPU UUID is present
- Requiring `metrics.json`
- An Agent SSH shell tool
- Bootstrapping `gemcp-node` over SSH
- Jump hosts or MFA
