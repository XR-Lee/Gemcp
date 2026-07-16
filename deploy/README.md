# Deployment

1. Build Gemcp once and run `gemcp keygen` and `gemcp bootstrap-token` on the server. Store both outputs only in the protected `.env` file.
2. Copy `../.env.example` to `.env` in this directory.
3. Replace `GEMCP_MASTER_KEY`, `GEMCP_BOOTSTRAP_TOKEN`, and both PostgreSQL password occurrences.
4. Restrict the file with `chmod 600 .env`.
5. Run `docker compose up -d --build`.
6. Configure Cloudflare Tunnel to forward the public hostname to `http://127.0.0.1:8080`.
7. Verify `/healthz` and `/readyz` through the public hostname.

The Compose file binds Gemcp only to loopback. PostgreSQL has no published host port.

Automated off-host backup is intentionally outside the single-user trial. Preserve `postgres_data` and the future Gemcp master-key file during manual server backup.
