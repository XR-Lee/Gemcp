# Deployment

1. Build Gemcp once and run `gemcp keygen` and `gemcp bootstrap-token` on the server. Store both outputs only in the protected `.env` file.
2. Copy `../.env.example` to `.env` in this directory.
3. Replace `GEMCP_MASTER_KEY`, `GEMCP_BOOTSTRAP_TOKEN`, and both PostgreSQL password occurrences. Set `GEMCP_VERSION`, the full immutable `GEMCP_COMMIT`, and `GEMCP_BUILT_AT` for release metadata.
4. Restrict the file with `chmod 600 .env`.
5. Choose the PostgreSQL bind root. `GEMCP_POSTGRES_DATA_DIR` defaults to `./postgres_data`; for an in-place source checkout, prefer an absolute protected path outside the Git worktree so Go and Docker build traversal never touches the PostgreSQL-owned `0700` directory. Create the chosen directory with mode `0700`. The Compose PostgreSQL entrypoint changes only this root to the image's `postgres` user while retaining mode `0700`, then delegates to the official entrypoint.
6. Run `docker compose up -d --build`.
7. Configure Cloudflare Tunnel to forward the public hostname to `http://127.0.0.1:8080`.
8. Keep `GEMCP_SCHEDULER_ENABLED=false` through first startup and migration.
9. Verify `/healthz` and `/readyz` through the public hostname.
10. Log in and verify `/api/v1/runtime/status` shows recent scheduler, Watchdog, and notification-worker heartbeats.
11. Configure and test SMTP, review existing queued experiments, then explicitly enable scheduling and recreate only the controlplane so the changed environment is loaded: `docker compose up -d --no-deps --force-recreate controlplane`.

The Compose file binds Gemcp only to loopback. PostgreSQL has no published host port. Relative `GEMCP_POSTGRES_DATA_DIR` values resolve from this deployment directory. The `watchdog` uses the same immutable image but runs `gemcp watchdog` independently, with no HTTP listener and no Provider create capability. Do not deploy the controlplane execution path without this service.

Read [Execution and shutdown enforcement](../docs/execution.md) for the complete arming and recovery model.

## PostgreSQL 18 volume upgrade

The official PostgreSQL 18 image moved its data root to `/var/lib/postgresql`. Gemcp `v0.6.0` corrects the Compose bind mount accordingly, and `v0.6.1` ensures a pre-created `0700` bind root is owned by the image's `postgres` user before the official entrypoint drops privileges.

PostgreSQL 18 explicitly refuses to start when `/var/lib/postgresql/data` is mounted as in older Gemcp Compose files. Do not recreate or remove an existing old PostgreSQL container before obtaining a dump. If the old stack is still running, dump it first:

```bash
docker compose exec -T postgres sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc' > gemcp-pre-v0.6.0.dump
```

Do not run `docker compose down` until that dump exists and `docker compose exec -T postgres pg_restore --list < gemcp-pre-v0.6.0.dump` succeeds. If the old container is stopped and no verified dump exists, preserve the container and its writable layer while investigating; the old `postgres_data` bind directory alone might not contain the PostgreSQL 18 cluster.

Preserve both the verified dump and the old `postgres_data` directory. After updating the Compose file, initialize the corrected mount, restore, then start the application so Ent can add the `v0.6.0` schema. The patched Compose entrypoint converts the newly created bind root to the container's `postgres` owner while keeping mode `0700`:

```bash
docker compose down
mv postgres_data postgres_data.pre-v0.6.0
mkdir -m 700 postgres_data
docker compose up -d postgres
docker compose exec -T postgres sh -c 'pg_restore --clean --if-exists -U "$POSTGRES_USER" -d "$POSTGRES_DB"' < gemcp-pre-v0.6.0.dump
docker compose up -d controlplane watchdog
```

Never use `docker compose down -v` during this procedure.

Automated off-host backup is intentionally outside the single-user trial. Preserve `postgres_data` and the protected deployment `.env` containing the Gemcp master key during manual server backup. Store the backup with access controls appropriate for production credentials.
