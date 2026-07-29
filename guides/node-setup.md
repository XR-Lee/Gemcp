# Gemcp Node Setup

[中文]({{GEMCP_PUBLIC_URL}}/node/setup?lang=zh) | **English**

This page is the complete coding-Agent handoff for an Owner-approved trusted Self-hosted GPU PC.

## Release identity

- Control plane: `{{GEMCP_PUBLIC_URL}}`
- Required release: `v{{GEMCP_VERSION}}`
- Required commit: `{{GEMCP_COMMIT}}`
- Repository: `git@github.com:XR-Lee/Gemcp.git`

The node binary and control plane must use the same release. The repository is private, so the GPU host or its coding Agent needs independent read access to GitHub. The Node Setup Link does not grant repository access.

## Setup-link boundary

The complete `{{GEMCP_PUBLIC_URL}}/node/setup?lang=en#code=...` link is a short-lived, single-use bearer capability. The optional `lang` query selects only the guide language. The setup code is stored only in the URL fragment and is not sent when this Markdown page is fetched.

- Do not send the complete link to Web search, Web fetch, issue trackers, shell history, logs, or command-line arguments.
- A coding Agent may read this public page before the enrollment exists.
- After prerequisites and the binary are ready, run the installer and let the Owner paste the complete link into its interactive prompt.
- If a trusted local Agent is explicitly given the complete link, it may pass it only through installer standard input and must immediately discard it.

## Agent execution contract

Before changing the host, inspect and report the current state. Do not install or upgrade the NVIDIA Driver unless the Owner separately approves that change. The Gemcp installer does not install or modify the GPU Driver, Docker Engine, or NVIDIA Container Toolkit.

Verify all of the following:

- Linux x86_64 with systemd.
- Exactly one supported NVIDIA GPU in the initial release.
- A working NVIDIA Driver and `nvidia-smi`.
- Docker Engine is running.
- NVIDIA Container Toolkit is configured and Docker can access the GPU.
- Go 1.26.5 is available to build from source.
- `/var/lib/gemcp-node/storage` is writable and has sufficient capacity for source trees, outputs, and complete logs.

If a prerequisite is missing, storage is insufficient, or installing it would require a driver change, stop and report the blocker before continuing.

## Build and verify

From the GPU host:

```bash
git clone --branch "v{{GEMCP_VERSION}}" --depth 1 \
  git@github.com:XR-Lee/Gemcp.git Gemcp
cd Gemcp
test "$(git rev-parse HEAD)" = "{{GEMCP_COMMIT}}"
make build-node
./bin/gemcp-node version
file ./bin/gemcp-node
```

The version output must identify `{{GEMCP_VERSION}}` and the expected commit prefix. The `file` output must report a statically linked Linux x86-64 executable.

## Install and enroll

The installer creates a dedicated system user, installs the static binary and systemd unit, runs diagnostics, and reads the complete Setup Link without placing it in process arguments:

```bash
sudo GEMCP_NODE_STORAGE_ROOT=/var/lib/gemcp-node/storage \
  GEMCP_NODE_BINARY=./bin/gemcp-node \
  ./deploy/install-gemcp-node.sh
```

Paste the complete Setup Link only when the installer prompts. It prints a Node ID and short pairing code, never the Node Token. Report the Node ID, pairing code, hostname, GPU UUID and model, and `systemctl status gemcp-node` to the Owner. Do not report the Node Token.

The node must remain `pending_verification` until the Owner compares the pairing code and hardware in **Nodes -> Enrollment activity**, selects the authorized Projects, and approves it. After approval, confirm that the daemon becomes active and continues sending heartbeats.

## Runtime configuration

In **Nodes -> Runtime configuration**, select the Project and create a runtime with:

- A public OCI image reference pinned with `@sha256:<64-hex-digest>`.
- The exact GPU model names accepted by the profile. Reported node models are prefilled.
- CPU and host-memory limits for the container.
- An optional switch to make the Environment and Resource Profile the Project defaults.

Agents can discover the resulting Environment and Resource Profile IDs through the existing Project options MCP tool. Self-hosted Experiments reserve zero CNY and remain subject to Project and global concurrency limits.

## Upgrade an enrolled Node

Do not run the enrollment installer again for an existing Node. In the Owner console, open **Nodes -> Machines -> Upgrade instructions** for the target Node and copy the release-bound handoff to a trusted coding Agent on that host. The handoff verifies the exact release commit, builds a static candidate, checks that no managed workload container remains before cleanup, and invokes `deploy/upgrade-gemcp-node.sh`.

The upgrade script preserves `/etc/gemcp-node/config.json`, `/etc/gemcp-node/credential`, `/var/lib/gemcp-node/state.db`, and the managed storage root. It atomically replaces only `/usr/local/bin/gemcp-node`, restarts the existing systemd service, and restores `/usr/local/bin/gemcp-node.previous` if the new daemon does not remain active. It never enrolls a new Node or prints the Node credential.

## Diagnostics and operation

```bash
sudo -u gemcp-node /usr/local/bin/gemcp-node doctor \
  --storage-root /var/lib/gemcp-node/storage

sudo systemctl status gemcp-node
sudo journalctl -u gemcp-node
```

The node initiates all control traffic to `{{GEMCP_PUBLIC_URL}}` over HTTPS. It does not expose MCP, SSH, or a control port.

Workload containers receive only the verified source tree, a managed output directory, and fixed resource constraints. They do not receive the Node Token, an Attempt Token, the Docker socket, or host paths selected by an Agent.
