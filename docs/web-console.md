# Owner Web console

The embedded Web application uses the same origin as the Go API and has three startup states:

1. First-run setup when PostgreSQL has no Owner.
2. Owner login when setup is complete and no valid Session exists.
3. The operations console for an authenticated Owner.

The setup flow collects the bootstrap credential, Owner account, and initial project policy. Its Compute step can either configure an encrypted AutoDL credential, approved image, and default resource profile or explicitly skip AutoDL and finish setup without Provider credentials. When configured, the step selects Public Elastic or Private Cloud so the Token is sent only to the matching official API host and the Environment and Resource Profile receive the matching execution backend. Private Cloud remains the default; Public Elastic requires an enterprise-verified AutoDL account. A skipped Provider can be added later in Lab → Provider. The final response displays the first Agent Token once.

The operations console provides:

- Scheduler, Watchdog, and notification-worker heartbeat visibility.
- Live Public Elastic or Private Cloud GPU, image, deployment, container, cache, and event visibility with a session-memory snapshot and visible-page 60-second background refresh. Public Elastic additionally displays wallet values and regional inventory; only Private Cloud attempts optional system-image discovery.
- A strict distinction between Gemcp-managed and external Provider deployments.
- Owner stop for managed deployments and phrase-confirmed emergency stop for all active managed resources.
- Validate-before-commit Provider Token rotation without any credential reveal path.
- Current project hard-budget totals and queued or active experiment counts.
- Recent immutable experiment lists, Attempt history, output paths, log tails, and metrics.
- Prepared-proposal review in Evidence, including repository, access (public HTTPS or Deploy Key), remote URL, Graph origin, dataset, named workload, working directory, dependency install, full commit SHA, argv, runtime, resource, preflight checks, expiry, and budget reservation. The Owner can also prepare a proposal from that page without an Agent Token. The Owner confirms the exact digest to start the Experiment; there is no blanket within-budget auto-approval.
- Project limits, including Owner PATCH of runtime and budget policy, AutoDL dataset bindings under `/root/autodl-fs/` for Public Elastic and Private Cloud, and private GitHub repository registration and verification. Probe and train require a registered binding; smoke may warn and continue.
- **Agent Readiness** on Research and Agents: Project-scoped Agent↔compute binding, usable resources, and heartbeats (`last_used_at`, Cloud SSH `last_probed_at`, Self-hosted `last_seen_at`, plus scheduler/watchdog). **Copy readiness** tells the Agent to call `get_project_options` / `get_experiment` and lists the Owner snapshot. Next actions open Handshake, grant `operate_nodes`, or Nodes. Agents are not exclusively bound to one node.
- One-time MCP setup links with claim/completion status, automated MCP verification for Pi, Codex, OpenCode, Claude Code, and Grok, scoped Agent Token activation and revocation, plus advanced one-time MCP JSON export. **Handshake prompt** on Agents creates that setup link and a single Agent message covering MCP enablement, Cloud SSH registration when `operate_nodes` is granted, and `prepare_experiment` from the repository. The Nodes Cloud SSH table copies the same prompt bound to a host; if the Agent has no MCP yet, that dialog sends the Owner to Agents to create the link-bearing copy.
- Encrypted SMTP configuration and durable notification delivery history.
- An **Images** Lab workspace, sibling of Diagnostics, for AutoDL Pro image bake requests. Agents may only request through MCP `configure`. Owner digest confirmation is the only start of Pro. This tree's bake provider is fail-closed, so Confirm marks Lab `failed` and does not invent an `image_uuid`. Failed bakes stay Lab status and never write Graph results. A finished `image_uuid` registers through the existing Environment form.
- A Nodes laboratory for Self-hosted enrollment and, when enabled, experimental Cloud SSH instances. Cloud SSH shows who registered the host, whether a process is running, and whether probe succeeded. It stores only encrypted SSH credentials and never renders a password, PEM, or locked image. Agent Tokens may grant `operate_nodes` so an Agent can register those hosts.

Provider controls operate only on persisted Gemcp ownership records. External deployments stay visible but cannot be stopped from Gemcp. Agent MCP tools remain the Agent submission and cancellation boundary. The Owner console can prepare a proposal without fabricating an Agent Token, confirm a prepared digest, start that Experiment, and edit Project runtime policy, but it does not expose arbitrary machine commands.

## Browser security

Owner authentication uses an HttpOnly, Secure, SameSite=Strict Session cookie. State-changing API requests also require the CSRF token issued at login. API and MCP responses use `Cache-Control: no-store`; first-run and newly issued Agent Tokens, one-time MCP exports, and encrypted-credential inputs must not be cached. Closing the reveal dialog clears its plaintext Token and config from application state.

The responsive UI is covered by mocked Playwright tests for desktop and mobile Setup, login, project, repository, MCP setup-link issuance, Agent Token export/revocation, prepared-proposal Owner confirmation, experiment and Attempt details, live Provider resources, managed stop, emergency stop, Token rotation, SMTP settings, notification history, runtime health, and bottom-navigation states:

```bash
cd frontend
npm ci --include=dev
npx playwright install chromium
npm run test:e2e

# When port 5187 is occupied by the production origin or file watchers are constrained:
npm run test:e2e:validation
```
