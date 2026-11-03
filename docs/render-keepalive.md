# VPS Render keepalive

The VPS uses a systemd timer to request the staging API's `/health/live` endpoint every ten minutes, with up to thirty seconds of scheduling tolerance. It sends no credentials, creates no data, and does not query PostgreSQL. No extra Render service is provisioned. The first request runs shortly after VPS boot; the timer is enabled across reboots.

Requests have a ten-second connection timeout and seventy-five-second overall timeout to allow a sleeping API to wake. Non-2xx/3xx responses and network failures fail the service and appear in the journal; the next scheduled run still occurs. Executions do not overlap. This is a keepalive, not an alerting service or an uptime guarantee.

Keeping the API awake consumes Render's included instance hours: 750 hours per workspace per month, shared across its free web services. It does not prevent maintenance, deployments, quota suspension, or the free database's thirty-day expiration. See [Render's free-tier limits](https://render.com/docs/free).

Source units are in `ops/systemd/`. Install on the existing VPS:

```sh
sudo install -m 644 ops/systemd/city4christ-render-keepalive.service /etc/systemd/system/
sudo install -m 644 ops/systemd/city4christ-render-keepalive.timer /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now city4christ-render-keepalive.timer
```

Check or run it manually:

```sh
systemctl list-timers city4christ-render-keepalive.timer
sudo systemctl start city4christ-render-keepalive.service
sudo journalctl -u city4christ-render-keepalive.service -n 10 --no-pager
```

Stop scheduled requests:

```sh
sudo systemctl disable --now city4christ-render-keepalive.timer
```

The timer depends on this VPS remaining available. If the API URL changes, update the unit and reload systemd. This timer is separate from the OTP email worker; stopping either does not stop the other.
