# Gemcp Node Setup

This page describes the Owner-approved enrollment flow for a trusted Self-hosted GPU PC. The complete setup link is a short-lived bearer capability. Do not print it, commit it, or send it to a Web-fetch or search service.

The setup code is stored only in the URL fragment. Opening this public page does not claim the enrollment.

## Prerequisites

- Linux x86_64 with systemd.
- Exactly one supported NVIDIA GPU in the initial release.
- A working NVIDIA Driver and `nvidia-smi`.
- Docker Engine with access for the `gemcp-node` service user.
- NVIDIA Container Toolkit already configured.
- A writable managed storage root with sufficient free space.

The Gemcp installer does not install or modify the GPU Driver, Docker, or NVIDIA Container Toolkit.

## Enrollment

Use the `gemcp-node` binary from the same Gemcp release as the control plane. The repository includes `deploy/install-gemcp-node.sh`, which installs the binary and systemd unit, creates the dedicated service user, runs diagnostics, and reads the complete setup link without placing it in process arguments.

From a verified Gemcp release checkout:

```bash
make build-node
sudo GEMCP_NODE_STORAGE_ROOT=/mnt/gemcp \
  GEMCP_NODE_BINARY=./bin/gemcp-node \
  ./deploy/install-gemcp-node.sh
```

Paste the complete `{{GEMCP_PUBLIC_URL}}/node/setup#code=...` link only when the installer prompts. It prints a node ID and short pairing code, never the Node Token.

The Owner must compare the pairing code and reported hardware in the Gemcp console, select the authorized Projects, and approve the node. The daemon remains `pending_verification` until approval.

## Runtime configuration

In **Nodes → Runtime configuration**, select the Project and create a runtime with:

- A public OCI image reference pinned with `@sha256:<64-hex-digest>`.
- The exact GPU model names accepted by the profile. Reported node models are prefilled.
- CPU and host-memory limits for the container.
- An optional switch to make the Environment and Resource Profile the Project defaults.

Agents can discover the resulting Environment and Resource Profile IDs through the existing Project options MCP tool. Self-hosted Experiments reserve zero CNY and remain subject to Project and global concurrency limits.

## Diagnostics and operation

```bash
sudo -u gemcp-node /usr/local/bin/gemcp-node doctor \
  --storage-root /mnt/gemcp

sudo systemctl status gemcp-node
sudo journalctl -u gemcp-node
```

The node initiates all control traffic to `{{GEMCP_PUBLIC_URL}}` over HTTPS. It does not expose MCP, SSH, or a control port.

Workload containers receive only the verified source tree, a managed output directory, and fixed resource constraints. They do not receive the Node Token, an Attempt Token, the Docker socket, or host paths selected by an Agent.
