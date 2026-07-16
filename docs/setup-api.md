# First-run setup and Web authentication

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
    "name": "AutoDL",
    "base_url": "https://api.autodl.com",
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
    "image_uuid": "existing-autodl-image-uuid"
  },
  "resource_profile": {
    "name": "default",
    "region": "westDC2",
    "gpu_names": ["RTX 4090"],
    "gpu_num": 1,
    "cuda_from": 118,
    "cuda_to": 128,
    "cpu_from": 1,
    "cpu_to": 128,
    "memory_from_gb": 1,
    "memory_to_gb": 512,
    "price_from_milli": 10,
    "price_to_milli": 3000,
    "reuse_container": false
  },
  "agent_token_label": "default-agent"
}
```

Money fields use milli-CNY. The Provider Token is AES-256-GCM encrypted before insertion. Agent and Session Tokens are stored as HMAC-SHA-256 digests.

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
