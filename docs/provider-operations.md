# Private Cloud Provider operations

Gemcp `v0.5.0` adds an Owner-only, live AutoDL Private Cloud resource view. The control plane decrypts the configured Developer Token only inside the server process, queries `https://private.autodl.com`, and returns a normalized credential-free response to the Web console.

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
- Private Cloud system images and CUDA metadata.
- Deployments and replica/starting/running/finished/failed counters.
- Active containers.
- Released containers, including reusable `in_cache` entries.

A successful query marks the account `backend=private`, `status=active`, and updates `last_validated_at`. A failed live query does not replace the credential. Collection follows Provider pagination up to 10 pages of 100 records per category; responses explicitly list any category truncated at that 1,000-record safety boundary.

### Deployment details

```http
GET /api/v1/provider/deployments/:deployment-id
```

Returns the selected deployment, active and released containers, and lifecycle events. The deployment ID is validated before it reaches the Provider API.

### Validate and rotate credential

```http
PUT /api/v1/provider
Content-Type: application/json
X-CSRF-Token: <session CSRF token>

{
  "name": "AutoDL Private Cloud",
  "base_url": "https://private.autodl.com",
  "token": "entered-in-the-private-owner-form"
}
```

Only the official Private Cloud host is accepted. Gemcp first queries images, system images, GPU stock, deployments, active containers, and released containers with the candidate Token. It encrypts and commits the Token only after every required query succeeds. The transaction also writes a `provider.credential_rotated` audit event that contains no Token material.

## Web console

The `Provider` navigation view contains:

- Connection state and last validation time.
- Live GPU capacity.
- Private and system image inventory.
- Deployment completion counters.
- Active and released container inventory.
- Reusable cache count.
- Deployment container and event details.
- Validate-and-rotate credential dialog.

The resource view is operational observability only. `v0.5.0` does not create, stop, delete, or mutate Provider resources from the Web console. Durable experiment execution remains disabled until the scheduler, Runner, and independent Watchdog are delivered in `v0.6.0`.

## Live integration test

The normal test suite skips real Provider access. An operator can explicitly run the read-only live contract with a protected Token file:

```bash
GEMCP_TEST_PRIVATE_TOKEN_FILE=/secure/path/to/token \
  go test -run '^TestLivePrivateCloudResources$' -v ./internal/provider
```

The test encrypts the Token into a temporary SQLite database, exercises the production Provider service, and rejects any serialized response containing the Token or credential ciphertext. It never creates compute resources.
