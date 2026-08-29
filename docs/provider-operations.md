# AutoDL Provider operations

Gemcp provides an Owner-only live resource view for AutoDL Public Cloud (`https://api.autodl.com`) and Private Cloud (`https://private.autodl.com`). The controlplane decrypts the configured Developer Token only inside the server process, queries the selected official host, and returns a normalized credential-free response. It identifies persisted Gemcp-owned resources and permits bounded lifecycle operations on those resources only.

## Security boundary

- The Provider Token is AES-256-GCM encrypted in PostgreSQL with `gemcp:provider-token:v1` associated data.
- API and Web responses never return the plaintext Token or encrypted ciphertext.
- Token rotation requires an authenticated Owner Session and CSRF header.
- A replacement Token must complete all read-only Provider queries before it replaces the existing credential.
- Container responses are decoded into an allowlisted type that excludes `info.root_password`, `info.ssh_command`, service URLs, and proxy credentials.
- Provider API errors are sanitized before reaching the browser. Server logs also use the AutoDL client's Token-redacting error path.
- All `/api/` responses remain `Cache-Control: no-store`.

The UI displays only whether a credential is configured, the configured official API host, Provider state, and validation timestamps. There is no reveal or copy operation for the Provider Token.

## Owner API

### Provider summary

```http
GET /api/v1/provider
```

Returns Provider identity, backend, status, base URL, credential-presence boolean, and validation timestamps without making a Provider network request.

### Live resource query

```http
POST /api/v1/provider/query
X-CSRF-Token: <session CSRF token>
```

The response contains a timestamped snapshot of:

- GPU model, idle count, and total count.
- User-private images.
- Private Cloud system images and CUDA metadata when the optional Web-console endpoint accepts Developer Token authentication. Public Cloud does not call that browser-session endpoint.
- Deployments and replica/starting/running/finished/failed counters.
- Active containers.
- Released containers, including reusable `in_cache` entries.

A successful query preserves the account's validated `elastic` or `private` backend, or binds a legacy `unverified` account from its stored official URL. It marks the account `status=active` and updates `last_validated_at`. A failed live query does not replace the credential. Collection follows Provider pagination up to 10 pages of 100 records per category; responses explicitly list any category truncated at that 1,000-record safety boundary.

Private Cloud reads its non-regional GPU stock directly and labels it with the `private` sentinel. Public Cloud queries each distinct region used by an active `autodl_elastic` Resource Profile in the tenant and returns region-scoped GPU rows. Proposal preflight counts only the selected profile region; Provider-page totals sum all displayed rows. Public Cloud therefore requires at least one configured Elastic profile to produce regional stock. Both backends collect user-private images, deployments, active containers, and released containers through documented Developer APIs. Private Cloud additionally attempts `/api/v2/image/list`; browser-session rejection truncates only `system_images` and does not fail the documented resource query.

### Deployment details

```http
GET /api/v1/provider/deployments/:deployment-id
```

Returns the selected deployment, active and released containers, and lifecycle events. The deployment ID is validated before it reaches the Provider API.

### Managed resources and stop

```http
GET  /api/v1/provider/managed-resources
POST /api/v1/provider/deployments/:deployment-id/stop
POST /api/v1/provider/emergency-stop
```

The managed list comes from PostgreSQL ownership records, not name-prefix inference. Stop requires an active owned deployment; external Provider resources return not found. The request persists stop intent for both scheduler and Watchdog and is idempotent.

Emergency stop requires `{"confirmation":"STOP"}`. It marks every active owned resource for immediate stop, preserves any stronger existing stop reason, and records an audit event and critical notification. It does not stop unknown or external Provider resources.

### Validate and rotate credential

```http
PUT /api/v1/provider
Content-Type: application/json
X-CSRF-Token: <session CSRF token>

{
  "name": "AutoDL Public Cloud",
  "base_url": "https://api.autodl.com",
  "token": "entered-in-the-private-owner-form"
}
```

Only the two official hosts shown above are accepted. An `unverified` account is bound to the selected host on first validation; later Token rotation must retain that backend because Environment and Resource Profile backend bindings are immutable. Gemcp first queries the resources required for that backend with the candidate Token. For Private Cloud it also attempts the optional system-image query. The Token is encrypted and committed only after every required documented Developer API query succeeds; browser-session rejection from the optional Private system-image endpoint does not invalidate an otherwise valid Developer Token. The transaction also writes a `provider.credential_rotated` audit event that contains no Token material.

## Web console

The Provider view keeps the most recent normalized snapshot in Vue memory for the current authenticated console session. Switching views immediately reuses a snapshot younger than 60 seconds. While the Provider view is active and the document is visible, it refreshes every 60 seconds; leaving the view or hiding the document pauses polling. Manual refresh resets the interval. Failed background refreshes preserve the prior snapshot and surface an error.

This is not HTTP or persistent browser caching: API responses remain `no-store`, and the snapshot is discarded on logout or page reload. No Provider Token, ciphertext, container access field, or other excluded credential enters the cache.

The `Provider` navigation view contains:

- Connection state and last validation time.
- Live GPU capacity, aggregated across active configured regions for Public Elastic.
- Private and system image inventory.
- Deployment completion counters with Managed or External ownership labels.
- Active and released container inventory and reusable cache count.
- Managed deployment stop and phrase-confirmed emergency stop.
- Deployment container and event details.
- Validate-and-rotate credential dialog.

Create remains an internal scheduler operation. The Web console never exposes arbitrary Provider create, shell, password, SSH, URL, or credential controls.

## Live integration test

The normal test suite skips real Provider access and never creates paid resources. An operator can explicitly run the read-only live contract with a protected Token file:

```bash
GEMCP_TEST_PRIVATE_TOKEN_FILE=/secure/path/to/token \
  go test -run '^TestLivePrivateCloudResources$' -v ./internal/provider
```

The test encrypts the Token into a temporary SQLite database, exercises the production Provider service, and rejects any serialized response containing the Token or credential ciphertext. It never creates compute resources.

The automated Public Elastic coverage uses a contract fake and never spends Provider funds. A real paid Elastic Job should be attempted only through the normal explicitly armed scheduler and budget/cleanup controls.
