# VPS staging email worker

The API runs on Render. The email worker runs on the existing Ubuntu VPS as `city4christ-email-worker.service`, under user `ubuntu`. It requires no public inbound HTTP or SMTP port; it connects outbound to PostgreSQL using verified TLS and to Resend over HTTPS.

The executable is `/home/ubuntu/.local/bin/city4christ-email-worker`. The private environment file is `/home/ubuntu/.config/city4christ/email.env`, mode 600, outside Git. Configure DATABASE_URL with the Render external hostname, `sslmode=verify-full`, and `sslrootcert=/etc/ssl/certs/ca-certificates.crt`. Use the same VERIFICATION_KEY_BASE64 as the API. Store the Resend key privately and retain the configured recipient policy. An explicit wildcard enables open beta delivery; do not silently change that policy during infrastructure repairs.

APP_ENV=staging and EMAIL_DELIVERY_MODE=resend enable delivery to allowlisted addresses only. EMAIL_DELIVERY_MODE=disabled keeps the service idle. Restart after configuration changes:

```sh
sudo systemctl restart city4christ-email-worker
systemctl is-active city4christ-email-worker
sudo journalctl -u city4christ-email-worker -f
```

The service is enabled at boot, restarts on failure, and never migrates, resets or seeds the database. Stop it with `sudo systemctl stop city4christ-email-worker` if delivery needs to pause immediately.

The database Blueprint allows only the VPS public IPv4 /32. A database session's `inet_client_addr()` can show Render's private proxy address; do not use that private address as the external allowlist rule. If the VPS public IP changes, update both Render Networking and the Blueprint before syncing. Do not allow all external IPs.

A confirmed received OTP verifies email delivery, not the whole public auth flow or that both components have matching keys. End-to-end acceptance must register through the actual HTTPS API, receive the message, verify its code through that API, and exercise login, refresh rotation/replay, `/me` and logout. Use isolated test accounts and credentials outside Git. Staging signup is currently open for beta testing under operator control; Blueprint updates must preserve that setting.

Free Render PostgreSQL still expires after 30 days. This worker does not remove that limit; arrange durable database hosting and backups before long-term use.

Appointment status emails and scheduled reminders are an opt-in extension of this same worker. See [appointment delivery and rollout](pastoral-email-delivery.md) before enabling them.
