# Owner Web console

The embedded Web application uses the same origin as the Go API and has three startup states:

1. First-run setup when PostgreSQL has no Owner.
2. Owner login when setup is complete and no valid Session exists.
3. The operations console for an authenticated Owner.

The setup flow collects the bootstrap credential, Owner account, encrypted AutoDL credential, initial project policy, approved image, and default resource profile. The Provider step explicitly selects Public Cloud or Private Cloud so the Token is sent only to the matching official API host. Private Cloud is the default after live phase-zero validation. Its final response displays the first Agent Token once.

The operations console provides:

- Scheduler, Watchdog, and notification-worker heartbeat visibility.
- Live Public Elastic or Private Cloud GPU, image, deployment, container, cache, and event visibility with a session-memory snapshot and visible-page 60-second background refresh.
- A strict distinction between Gemcp-managed and external Provider deployments.
- Owner stop for managed deployments and phrase-confirmed emergency stop for all active managed resources.
- Validate-before-commit Provider Token rotation without any credential reveal path.
- Current project hard-budget totals and queued or active experiment counts.
- Recent immutable experiment lists, Attempt history, output paths, log tails, and metrics.
- Project limits and private GitHub repository registration and verification.
- One-time Pi setup links with claim/completion status, automated MCP verification, scoped Agent Token activation and revocation, plus advanced one-time MCP JSON export.
- Encrypted SMTP configuration and durable notification delivery history.

Provider controls operate only on persisted Gemcp ownership records. External deployments stay visible but cannot be stopped from Gemcp. Agent MCP tools remain the submission and cancellation boundary; the Owner UI does not expose arbitrary machine commands.

## Browser security

Owner authentication uses an HttpOnly, Secure, SameSite=Strict Session cookie. State-changing API requests also require the CSRF token issued at login. API and MCP responses use `Cache-Control: no-store`; first-run and newly issued Agent Tokens, one-time MCP exports, and encrypted-credential inputs must not be cached. Closing the reveal dialog clears its plaintext Token and config from application state.

The responsive UI is covered by mocked Playwright tests for desktop and mobile Setup, login, project, repository, Pi setup-link issuance, Agent Token export/revocation, experiment and Attempt details, live Provider resources, managed stop, emergency stop, Token rotation, SMTP settings, notification history, runtime health, and bottom-navigation states:

```bash
cd frontend
npm ci --include=dev
npx playwright install chromium
npm run test:e2e

# When port 5187 is occupied by the production origin or file watchers are constrained:
npm run test:e2e:validation
```
