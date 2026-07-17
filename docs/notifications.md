# SMTP notifications

Gemcp records critical operational notifications in PostgreSQL before attempting SMTP delivery. Provider failures, budget stops, runtime extensions, timeout enforcement, watchdog shutdown, and emergency stop therefore remain visible even when mail delivery is unavailable.

## Configure

Open **Alerts** in the Owner console and select **SMTP settings**. Configure one TLS-capable relay and at least one recipient. Passwords are AES-256-GCM encrypted with tenant-specific authenticated data and are never returned by the API or UI.

Leaving the password field blank preserves an existing stored password. Use **Remove stored password** to clear it explicitly. Gemcp validates address syntax and requires either implicit TLS or STARTTLS; plaintext SMTP is rejected.

The equivalent Owner endpoints are:

```text
GET  /api/v1/notifications/settings
PUT  /api/v1/notifications/settings
GET  /api/v1/notifications
POST /api/v1/notifications/test
```

All mutating calls require the Owner Session and CSRF header.

## Delivery model

The controlplane notification worker claims pending rows with PostgreSQL row locking, sends outside the transaction, and records the result. Delivery is at least once: a process failure after SMTP accepts a message but before PostgreSQL records success can produce a duplicate.

A failed delivery is retried with bounded backoff. After five attempts it remains durably `failed` for operator inspection. Error text is bounded and redacts the configured SMTP password before storage. The outbox and delivery history are not automatically deleted.

The worker writes a service heartbeat on every poll. Overview and `GET /api/v1/runtime/status` consider it healthy when the heartbeat is recent. A disabled SMTP setting still has a healthy worker; queued notifications remain pending until delivery is enabled.

Use **Send test** after configuration, then confirm both SMTP receipt and a `sent` delivery-history row before arming the experiment scheduler.
