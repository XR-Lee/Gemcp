# AutoDL Provider operations

A tenant can bind Private Cloud and Public Elastic at the same time. Each backend keeps its own encrypted Developer Token, official host, and live snapshot. Adding or rotating one account does not replace the other.

Gemcp provides an Owner-only live resource view for the configured AutoDL execution backends:

- Public Elastic at `https://api.autodl.com`, stored as Provider backend `elastic` and execution backend `autodl_elastic`;
- Private Cloud at `https://private.autodl.com`, stored as Provider backend `private` and execution backend `autodl_private`.

Public Pro remains available only to the phase-zero read probe. It is not a production scheduling fallback because Gemcp does not implement its instance create and cleanup lifecycle.

The controlplane decrypts the configured Developer Token only inside the server process and returns normalized credential-free responses. Gemcp identifies persisted owned resources and permits bounded lifecycle operations on those resources only.

## Security boundary

- The Provider Token is AES-256-GCM encrypted in PostgreSQL with `gemcp:provider-token:v1` associated data.
- API and Web responses never return the plaintext Token or encrypted ciphertext.
- Token rotation requires an authenticated Owner Session and CSRF header.
- The Developer Token is sent only to the selected official host in AutoDL's raw `Authorization` header; it is not prefixed with `Bearer`.
- A replacement Token must complete the required read-only queries for the selected backend before it replaces the existing credential.
- Container responses are decoded into an allowlisted type that excludes `info.root_password`, `info.ssh_command`, service URLs, and proxy credentials.
- Provider API errors are sanitized before reaching the browser. Server logs also use the AutoDL client's Token-redacting error path.
- All `/api/` responses remain `Cache-Control: no-store`.

The UI displays only whether a credential is configured, the configured official API host, Provider state, and validation timestamps. There is no reveal or copy operation for the Provider Token.

## Owner API

### Provider summary

```http
GET /api/v1/provider
```

Returns `{ "providers": [...] }`. Each item is Provider identity, backend, status, base URL, credential-presence boolean, and validation timestamps, without making a Provider network request. An empty list means no AutoDL account is bound yet.

### Live resource query

```http
POST /api/v1/provider/query?backend=elastic
X-CSRF-Token: <session CSRF token>
```

`backend` must be `private` or `elastic`. It is required when both accounts are bound. When only one account exists, the query parameter may be omitted.

The response contains a timestamped snapshot of:

- Public Elastic wallet `assets`, `accumulate`, and `voucher_balance` values in integer milli-CNY. Private Cloud omits `wallet`.
- GPU model, idle count, and total count. Public Elastic inventory is regional and is queried for regions used by active `autodl_elastic` Resource Profiles.
- User-private images.
- Private Cloud system images and CUDA metadata when the optional Web-console endpoint accepts Developer Token authentication.
- Deployments and replica/starting/running/finished/failed counters.
- Active containers.
- Released containers, including reusable `in_cache` entries.

A successful query marks the account `backend=elastic` or `backend=private`, sets `status=active`, and updates `last_validated_at`. A failed live query does not replace the credential. Collection follows Provider pagination up to 10 pages of 100 records per category; responses explicitly list any category truncated at that safety boundary.

Public Elastic GPU inventory requires one `region_sign` per request. The returned idle count is a count of individual GPUs, not proof that multiple idle GPUs are colocated on one machine. Public container listing also requires a deployment UUID, so Gemcp enumerates deployments first and then queries active and released containers per deployment. It never depends on an undocumented account-wide container query.

The public Developer API dynamically lists only user-private images. Official public base-image UUIDs come from AutoDL's documentation or console and therefore do not appear in `system_images`. Private Cloud additionally has `/api/v2/image/list`; when that optional endpoint rejects Developer Token authentication, Gemcp marks only `system_images` as truncated and continues collecting the documented Developer API resources.

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
  "name": "AutoDL Public Elastic",
  "base_url": "https://api.autodl.com",
  "backend": "elastic",
  "token": "entered-in-the-owner-form"
}
```

Only the two official execution hosts are accepted. An optional `backend` value must be `elastic` for `https://api.autodl.com` or `private` for `https://private.autodl.com`; when it is omitted, Gemcp infers the backend from the host. Callers cannot bind a Token to an arbitrary host or select Public Pro here.

For Public Elastic, the AutoDL account must be enterprise-verified before it can use the Elastic deployment API. Gemcp validates the candidate Token against user-private images, deployments, regional GPU inventory, and per-deployment containers. GPU inventory is queried only for regions already used by active `autodl_elastic` Resource Profiles. A successful Public Elastic bind creates or updates only the `elastic` Provider account. A successful Public Elastic rotation or first-time bind also creates a `public-elastic` Environment and Resource Profile on each active Project when a user-private image is visible. If a Private Cloud account is already bound, those public runtimes stay selectable and do not steal the Private Cloud defaults. If Public Elastic is the only AutoDL account, Gemcp makes that pair the default. For Private Cloud, Gemcp validates private images, non-regional GPU stock, deployments, and containers, and also attempts the optional system-image query. The Token is encrypted and committed only after the required Developer API queries succeed. Private Cloud browser-session rejection from the optional system-image endpoint does not invalidate an otherwise valid Developer Token. The transaction writes `provider.account_created` when the selected backend did not exist yet, or `provider.credential_rotated` when it updates that backend's existing credential. Neither event contains Token material.

AutoDL's public wallet endpoint reports integer milli-CNY values. The Provider view displays them for visibility, but they are not Gemcp's budget or invoice authority; the AutoDL console remains authoritative for actual balance and billing.

## Web console

The Provider view keeps the most recent normalized snapshot in Vue memory for the current authenticated console session. Switching views immediately reuses a snapshot younger than 60 seconds. While the Provider view is active and the document is visible, it refreshes every 60 seconds; leaving the view or hiding the document pauses polling. Manual refresh resets the interval. Failed background refreshes preserve the prior snapshot and surface an error.

This is not HTTP or persistent browser caching: API responses remain `no-store`, and the snapshot is discarded on logout or page reload. No Provider Token, ciphertext, container access field, or other excluded credential enters the cache.

The `Provider` navigation view contains:

- A Private Cloud / Public Cloud switcher, plus an add action for the backend that is not bound yet.
- Connection state and last validation time.
- Public Elastic wallet visibility when returned by the Provider.
- Live GPU capacity.
- User-private image inventory and, for Private Cloud only, best-effort system-image inventory.
- Deployment completion counters with Managed or External ownership labels.
- Active and released container inventory and reusable cache count.
- Managed deployment stop and phrase-confirmed emergency stop.
- Deployment container and event details.
- Validate-and-rotate credential dialog.

Create remains an internal scheduler operation. The Web console never exposes arbitrary Provider create, shell, password, SSH, URL, or credential controls.

## Live validation

The normal test suite skips real Provider access and never creates paid resources. Use the matching phase-zero read probe before enabling scheduling:

```bash
./bin/gemcp phase0 read --backend elastic --region westDC2
./bin/gemcp phase0 read --backend private
```

Region identifiers are Provider data and can change; validate the intended region against the current AutoDL documentation and API rather than treating an example value as permanent. The public read probe also checks wallet visibility. Public Elastic live Job probes additionally require enterprise eligibility and the explicit spend confirmation documented in [Phase-zero AutoDL validation](phase-zero.md).

An operator can explicitly run the existing Private Cloud read-only integration test with a protected Token file:

```bash
GEMCP_TEST_PRIVATE_TOKEN_FILE=/secure/path/to/token \
  go test -run '^TestLivePrivateCloudResources$' -v ./internal/provider
```

The test encrypts the Token into a temporary SQLite database, exercises the production Provider service, and rejects any serialized response containing the Token or credential ciphertext. It never creates compute resources.
