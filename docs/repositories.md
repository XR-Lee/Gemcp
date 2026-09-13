# Git repositories

Gemcp accepts GitHub repositories in either of these forms:

```text
https://github.com/owner/repository
git@github.com:owner/repository.git
```

Public repositories activate immediately. Gemcp fetches them over anonymous HTTPS and does not create a Deploy Key. Private repositories still generate a distinct Ed25519 Deploy Key. The private key is AES-256-GCM encrypted with repository-specific associated data. Only the public key is returned through the Owner API.

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

`ssh_url` remains accepted as an alias of `url`. A public repository returns `status=active` and `access=public_https`. A private repository returns `status=pending_key`, `access=ssh_deploy_key`, and `deploy_public_key`. Add that key to the GitHub repository as a read-only Deploy Key. Do not enable write access. Views include `observation_writes_allowed` and, while `pending_key` or `error`, `pending_note`: Graph and experiment-catalog observation writes remain possible; prepare of a git-backed Experiment still requires verify.

## Verify a private repository

Gemcp ships a release-pinned copy of GitHub's official Ed25519 host fingerprint. Normal GitHub onboarding does not ask the Owner to transcribe it. After installing the read-only Deploy Key:

```http
POST /api/v1/repositories/<repository-id>/verify
X-CSRF-Token: <csrf-token>
Content-Type: application/json

{}
```

Gemcp scans `github.com`, requires one returned host key to match the release-pinned fingerprint, and performs a noninteractive read-only Git fetch with an isolated HOME, no system Git configuration, strict host-key checking, and the repository Deploy Key. The repository becomes `active` only after that succeeds. The optional `host_key_fingerprint` request field remains an Advanced override for a separately reviewed pin; never derive it from untrusted `ssh-keyscan` output alone.

If GitHub rejects the key, the API returns the classified failure (`REPOSITORY_DEPLOY_KEYS_DISABLED`, `REPOSITORY_DEPLOY_KEY_MISSING`, or `REPOSITORY_VERIFICATION_FAILED`) and the full error text, including that observation writes remain possible while pending. The Owner console shows that message instead of a generic “add the key” note.

Public repositories skip this step. Later commit and archive fetches try anonymous GitHub HTTPS first and fall back to the Deploy Key only when HTTPS is unauthorized.

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
- [GitHub SSH key fingerprints](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/githubs-ssh-key-fingerprints)
