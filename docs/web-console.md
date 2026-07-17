# Owner Web console

The embedded Web application uses the same origin as the Go API and has three startup states:

1. First-run setup when PostgreSQL has no Owner.
2. Owner login when setup is complete and no valid Session exists.
3. The operations console for an authenticated Owner.

The setup flow collects the bootstrap credential, Owner account, encrypted AutoDL credential, initial project policy, approved image, and default resource profile. The Provider step explicitly selects Public Cloud or Private Cloud so the Token is sent only to the matching official API host. Private Cloud is the default after live phase-zero validation. Its final response displays the first Agent Token once.

The operations console provides:

- Live Private Cloud GPU, image, deployment, container, cache, and event visibility.
- Validate-before-commit Provider Token rotation without any credential reveal path.
- Current project hard-budget totals and queued or active experiment counts.
- Recent and filtered immutable experiment lists with specification details.
- Project limits and status.
- Private GitHub repository registration with generated Deploy public keys.
- Pinned host-key and read-access verification.

The console remains read-only for Provider and experiment lifecycle changes in `v0.5.0`, except for validating and rotating the encrypted Provider credential. Agent MCP tools remain the experiment submission and cancellation boundary. Provider execution controls remain disabled until the `v0.6.0` scheduler, Runner, and Watchdog are complete.

## Browser security

Owner authentication uses an HttpOnly, Secure, SameSite=Strict Session cookie. State-changing API requests also require the CSRF token issued at login. API and MCP responses use `Cache-Control: no-store`; the first-run Agent Token and encrypted-credential inputs must not be cached.

The responsive UI is covered by mocked Playwright tests for desktop and mobile Setup, login, project, repository, experiment, live Provider resources, Provider deployment details, Token rotation, and bottom-navigation states:

```bash
cd frontend
npm ci --include=dev
npx playwright install chromium
npm run test:e2e
```
