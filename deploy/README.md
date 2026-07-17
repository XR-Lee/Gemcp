# Deployment

1. Build Gemcp once and run `gemcp keygen` and `gemcp bootstrap-token` on the server. Store both outputs only in the protected `.env` file.
2. Copy `../.env.example` to `.env` in this directory.
3. Replace `GEMCP_MASTER_KEY`, `GEMCP_BOOTSTRAP_TOKEN`, and both PostgreSQL password occurrences. Set `GEMCP_VERSION`, the full immutable `GEMCP_COMMIT`, and `GEMCP_BUILT_AT` for release metadata.
4. Restrict the file with `chmod 600 .env`.
5. Run `docker compose up -d --build`.
6. Configure Cloudflare Tunnel to forward the public hostname to `http://127.0.0.1:8080`.
7. Keep `GEMCP_SCHEDULER_ENABLED=false` through first startup and migration.
8. Verify `/healthz` and `/readyz` through the public hostname.
9. Log in and verify `/api/v1/runtime/status` shows recent scheduler, Watchdog, and notification-worker heartbeats.
10. Configure and test SMTP, review existing queued experiments, then explicitly enable scheduling and recreate only the controlplane so the changed environment is loaded: `docker compose up -d --no-deps --force-recreate controlplane`.

The Compose file binds Gemcp only to loopback. PostgreSQL has no published host port. The `watchdog` uses the same immutable image but runs `gemcp watchdog` independently, with no HTTP listener and no Provider create capability. Do not deploy the controlplane execution path without this service.

Read [Execution and shutdown enforcement](../docs/execution.md) for the complete arming and recovery model.

## PostgreSQL 18 volume upgrade

The official PostgreSQL 18 image moved its data root to `/var/lib/postgresql`. Gemcp `v0.6.0` corrects the Compose bind mount accordingly. Before recreating a stack deployed with the older `/var/lib/postgresql/data` target, dump the still-running database:

```bash
docker compose exec -T postgres sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc' > gemcp-pre-v0.6.0.dump
```

Do not run `docker compose down` until that dump exists and `docker compose exec -T postgres pg_restore --list < gemcp-pre-v0.6.0.dump` succeeds. Preserve both the dump and the old `postgres_data` directory. After updating the Compose file, initialize the corrected mount, restore, then start the application so Ent can add the `v0.6.0` schema:

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
