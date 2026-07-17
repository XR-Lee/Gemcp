# First-run setup and Web authentication

The embedded Web application drives these endpoints automatically. The raw contract below is retained for recovery, testing, and non-browser administration.

Gemcp starts only when `GEMCP_MASTER_KEY` is a valid base64-encoded 32-byte key. Generate it locally on the control server:

```bash
gemcp keygen
```

Store it in the protected deployment `.env`; loss of this key makes encrypted Provider and Git credentials unrecoverable.

Generate a separate one-time bootstrap credential:

```bash
gemcp bootstrap-token
```

Set it as `GEMCP_BOOTSTRAP_TOKEN`. The setup request must send it in `X-Gemcp-Bootstrap-Token`; this prevents an Internet client from claiming an uninitialized deployment. It has no authority after setup completes.

## Setup status

```http
GET /api/v1/setup/status
```

The response is public so the embedded Web application can choose between setup and login. It contains only an `initialized` boolean.

## Initialize

`POST /api/v1/setup` is accepted exactly once. It requires `X-Gemcp-Bootstrap-Token`, runs as one serializable database transaction, and returns the first Agent Token only in this response.

```json
{
  "organization_name": "Research Lab",
  "owner": {
    "email": "owner@example.com",
    "password": "use-a-long-unique-password"
  },
  "provider": {
    "name": "AutoDL Private Cloud",
    "base_url": "https://private.autodl.com",
    "token": "set-through-the-private-setup-form"
  },
  "project": {
    "name": "First Project",
    "slug": "first-project",
    "monthly_budget_milli": 100000,
    "max_experiment_milli": 20000,
    "max_concurrency": 1,
    "max_runtime_seconds": 3600,
    "timeout_extension_seconds": 3600,
    "termination_grace_seconds": 60
  },
  "environment": {
    "name": "default",
    "image_uuid": "visible-private-or-system-image-uuid"
  },
  "resource_profile": {
    "name": "default",
    "region": "private",
    "gpu_names": ["NVIDIA GeForce RTX 3090"],
    "gpu_num": 1,
    "cuda_from": 118,
    "cuda_to": 118,
    "cpu_from": 1,
    "cpu_to": 128,
    "memory_from_gb": 1,
    "memory_to_gb": 512,
    "price_from_milli": 10,
    "price_to_milli": 9000,
    "reuse_container": false
  },
  "agent_token_label": "default-agent"
}
```

Money fields use milli-CNY. For the selected Private Cloud backend, `region` is the sentinel `private`, and the existing profile range stores the one validated `cuda_v` selector as equal `cuda_from` and `cuda_to` values. Public Elastic deployments continue to use a real region and CUDA range.

The Provider Token is AES-256-GCM encrypted before insertion. It is never returned by setup or later Provider APIs; the Owner console can only validate and replace it. Agent and Session Tokens are stored as HMAC-SHA-256 digests. After setup, Owners can issue replacement or additional project Agent Tokens through the [Agent Token API](agent-tokens.md); each plaintext is returned once.

## Login

```http
POST /api/v1/auth/login
Content-Type: application/json

{"email":"owner@example.com","password":"..."}
```

A successful response sets an `HttpOnly`, `SameSite=Strict` Session cookie and a readable CSRF cookie. State-changing authenticated requests must send the CSRF value in `X-CSRF-Token`.

```text
GET  /api/v1/auth/me
POST /api/v1/auth/logout
```

Production uses Secure cookies behind Cloudflare Tunnel. Set `GEMCP_SECURE_COOKIES=false` only for local HTTP development.
