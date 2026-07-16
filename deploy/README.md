# Deployment

1. Copy `../.env.example` to `.env` in this directory.
2. Replace both PostgreSQL password occurrences with the same random value.
3. Run `docker compose up -d --build`.
4. Configure Cloudflare Tunnel to forward the public hostname to `http://127.0.0.1:8080`.
5. Verify `/healthz` and `/readyz` through the public hostname.

The Compose file binds Gemcp only to loopback. PostgreSQL has no published host port.

Automated off-host backup is intentionally outside the single-user trial. Preserve `postgres_data` and the future Gemcp master-key file during manual server backup.
