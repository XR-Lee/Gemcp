# Private Git repositories

Gemcp currently accepts GitHub SSH repositories in this form:

```text
git@github.com:owner/repository.git
```

Each registration generates a distinct Ed25519 Deploy Key. The private key is AES-256-GCM encrypted with repository-specific associated data. Only the public key is returned through the Owner API.

## Register

Log in as Owner, retain the Session cookies, and send the CSRF value from the login response in `X-CSRF-Token` for each state-changing request.

Discover the project ID:

```http
GET /api/v1/projects
```

Create the repository:

```http
POST /api/v1/repositories
X-CSRF-Token: <csrf-token>
Content-Type: application/json

{
  "project_id": "project-uuid",
  "name": "training",
  "ssh_url": "git@github.com:owner/repository.git",
  "default_branch": "main"
}
```

The response includes `deploy_public_key`. Add it to that GitHub repository as a read-only Deploy Key. Do not enable write access.

## Pin and verify the host

Obtain GitHub's current SSH host-key fingerprint from the official GitHub documentation through an independently trusted HTTPS connection. Do not trust the output of `ssh-keyscan` by itself.

Then verify repository access:

```http
POST /api/v1/repositories/<repository-id>/verify
X-CSRF-Token: <csrf-token>
Content-Type: application/json

{"host_key_fingerprint":"SHA256:<trusted-fingerprint>"}
```

Gemcp scans `github.com`, requires one returned host key to match the pinned fingerprint, and performs a noninteractive read-only Git fetch with an isolated HOME, no system Git configuration, strict host-key checking, and the repository Deploy Key. The repository becomes `active` only after that succeeds.

List registrations and retrieve their public Deploy Keys:

```http
GET /api/v1/repositories?project_id=<project-uuid>
```

Experiment submission accepts only active repositories and verifies the requested immutable commit with another depth-one fetch before reserving budget.

Official references:

- [Managing deploy keys](https://docs.github.com/en/authentication/connecting-to-github-with-ssh/managing-deploy-keys)
- [GitHub SSH key fingerprints](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/githubs-ssh-key-fingerprints)
