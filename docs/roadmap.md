# Roadmap

Changelog-style notes as weeks land. Week 1 is shipped on `main`.

## Week 3 — Leases and recovery (planned)

- Worker registers in `workers` and sends heartbeats.
- Claim sets `lease_owner` and `lease_expires_at`; same-status updates renew the lease.
- Background recovery moves expired `running` jobs back to `pending` when attempts remain.
- Metrics: heartbeat gap, recovery lag, abandoned lease count.
