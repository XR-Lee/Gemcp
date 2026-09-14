# Git repositories

Gemcp accepts GitHub repositories in either of these forms:

```text
https://github.com/owner/repository
git@github.com:owner/repository.git
```

Public repositories activate immediately. Gemcp fetches them over anonymous HTTPS and does not create a Deploy Key. Private repositories still generate a distinct Ed25519 Deploy Key. The private key is AES-256-GCM encrypted with repository-specific associated data. Only the public key is returned through the Owner API.

When GitHub Deploy Keys are disabled, attach a write-only GitHub PAT or fine-grained token to that same repository record (`access=https_token`). A fine-grained token with **Contents: Read** on one repository is enough for `git ls-remote` and archive. The token is AES-256-GCM encrypted with repository-specific associated data (`gemcp:repository-https-token:v1:<repository-id>`). It is never returned in MCP or Owner API views, logs, or confirmation digests. Gemcp does not inherit host `SSH_AUTH_SOCK`.

## Register

Log in as Owner, retain the Session cookies, and send the CSRF value from the login response in `X-CSRF-Token` for each state-changing request.

Discover the project ID:

```http
GET /api/v1/projects
```

Create the repository. The name may be omitted; Gemcp derives it from the GitHub repository.

```http
POST /api/v1/repositories
X-CSRF-Token: <csrf-token>
Content-Type: application/json

{
  "project_id": "project-uuid",
  "url": "https://github.com/owner/repository",
  "default_branch": "main"
}
```

`ssh_url` remains accepted as an alias of `url`. A public repository returns `status=active` and `access=public_https`. A private repository returns `status=pending_key`, `access=ssh_deploy_key`, and `deploy_public_key`. Add that key to the GitHub repository as a read-only Deploy Key. Do not enable write access. If Deploy Keys are disabled, keep the pending record and continue with an HTTPS token (below). Views include `observation_writes_allowed` and, while `pending_key` or `error`, `pending_note`: Graph and experiment-catalog observation writes remain possible; prepare of a git-backed Experiment still requires verify.

## Verify a private repository

Gemcp ships a release-pinned copy of GitHub's official Ed25519 host fingerprint. Normal GitHub onboarding does not ask the Owner to transcribe it. After installing the read-only Deploy Key:

```http
POST /api/v1/repositories/<repository-id>/verify
X-CSRF-Token: <csrf-token>
Content-Type: application/json

{}
```

Gemcp scans `github.com`, requires one returned host key to match the release-pinned fingerprint, and performs a noninteractive read-only Git fetch with an isolated HOME, no system Git configuration, strict host-key checking, and the repository Deploy Key. The repository becomes `active` only after that succeeds. The optional `host_key_fingerprint` request field remains an Advanced override for a separately reviewed pin; never derive it from untrusted `ssh-keyscan` output alone.

If GitHub rejects the key, the API returns the classified failure (`REPOSITORY_DEPLOY_KEYS_DISABLED`, `REPOSITORY_DEPLOY_KEY_MISSING`, `REPOSITORY_HTTPS_TOKEN_INVALID`, or `REPOSITORY_VERIFICATION_FAILED`) and the full error text, including that observation writes remain possible while pending. Deploy Keys disabled errors point at configuring `https_token` instead of only “enable Deploy Keys”. The Owner console shows that message instead of a generic “add the key” note.

## Activate a private repository with an HTTPS token

Use this when Deploy Keys are disabled (GitHub 422) or when the Owner prefers a fine-grained PAT.

1. Register the private repository as usual. It stays `pending_key`.
2. Create a GitHub fine-grained personal access token with **Contents: Read** on that one repository (classic PATs also work; they are broader).
3. Owner console: paste the token in the write-only field and choose **Activate with HTTPS token**. MCP: call `verify_repository` with `repository_id` and write-only `https_token`.

```http
POST /api/v1/repositories/<repository-id>/verify
X-CSRF-Token: <csrf-token>
Content-Type: application/json

{
  "https_token": "<fine-grained-or-classic-token>"
}
```

A successful HTTPS `ls-remote` / fetch activates the record: `status=active`, `access=https_token`, `https_token_configured=true`. The token value is not in the response. Later `prepare_experiment` ResolveRef and archive use the token path and do not fall back to SSH (so a Deploy Keys 422 cannot mask a working token). Public HTTPS and Deploy Key paths are unchanged.

Public repositories skip this step. Later commit and archive fetches try anonymous GitHub HTTPS first. If an HTTPS token is stored, that token path is used and SSH is not tried. Otherwise Gemcp falls back to the Deploy Key only when anonymous HTTPS is unauthorized.

List registrations and retrieve their public Deploy Keys:

```http
GET /api/v1/repositories?project_id=<project-uuid>
```

Private repository views include `deploy_key_settings_url` pointing at `https://github.com/<owner>/<repository>/settings/keys`.

## Readiness

After register or verify, inspect access, the detected default branch, `gemcp.yaml` workload names, and whether the Project has a compatible Environment, Resource Profile, and Dataset Binding:

```http
GET /api/v1/repositories/<repository-id>/readiness?project_id=<project-uuid>
```

The report does not start a run. A missing `gemcp.yaml` is not a blocker; a pending Deploy Key or missing Project default is. When HEAD is a symbolic branch that differs from the stored default, readiness records the detected branch for later prepare.

Experiment submission accepts only active repositories and verifies the requested immutable commit with another depth-one fetch before reserving budget.

Official references:

- [Managing deploy keys](https://docs.github.com/en/authentication/connecting-to-github-with-ssh/managing-deploy-keys)
- [Managing personal access tokens](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens)
- [GitHub SSH key fingerprints](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/githubs-ssh-key-fingerprints)
