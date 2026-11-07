# Arden

Distributed job scheduler and execution engine. Postgres is the source of truth and the coordination mechanism: jobs are claimed with row locks, owned with a lease, and protected with a fencing token.

**Week 1** is the job API. It submits, reads, lists, and cancels work. It does not run jobs. Delivery, once workers exist, is at-least-once. See [docs/adr](docs/adr).

Portfolio: [brohammad.tech](https://brohammad.tech/#projects)

## What works

- `POST /v1/jobs` persists a job. Omit `scheduled_at` to make it due now, or send a future time to delay it.
- `Idempotency-Key` returns the original job when the request matches, and `409` when the same key is reused with a different body.
- `GET /v1/jobs/{id}` and `GET /v1/jobs` read current state. List filters are `status` and `queue`, with keyset pagination.
- `POST /v1/jobs/{id}/cancel` cancels a pending job. Cancelling it again is a no-op. A running job cannot be cancelled.
- The schema already enforces the state machine, lease shape, and fencing-token rules used by later weeks.

## What does not work yet

Workers, claiming, heartbeats, crash recovery, retries, dead-letter replay, cron, metrics, and tracing. Those are Weeks 2–7. No Redis, Kafka, or UI.

## Run

Requires Go 1.26 and Docker.

```bash
make up
make migrate
make run
```

`DATABASE_URL` defaults to `postgres://arden:arden@localhost:5432/arden?sslmode=disable`. `ADDR` defaults to `:8080`.

`docker compose -f deploy/docker-compose.yml up --build` starts Postgres and the API together. The API container applies migrations, then listens on port 8080.

```bash
curl -s -X POST localhost:8080/v1/jobs \
  -H 'content-type: application/json' \
  -H 'Idempotency-Key: welcome-email-42' \
  -d '{"queue":"email","payload":{"to":"a@example.com"},"max_attempts":5}'
```

`GET /healthz` does not touch the database. `GET /readyz` runs `SELECT 1`.

## Job shape

| Field | Meaning |
| --- | --- |
| `queue` | Worker selector. Default `default`. Pattern `^[a-z0-9][a-z0-9_-]{0,63}$`. |
| `payload` | JSON object, at most 256 KiB. |
| `priority` | Higher runs first. Default 0. |
| `scheduled_at` | Earliest run time. Default is now. |
| `max_attempts` | Claims allowed in the current cycle. Default 5, range 1–100. |
| `attempt` | Claims in the current cycle. Replay resets this to 0. |
| `fencing_token` | Increases by 1 on every claim and never goes back down. |
| `status` | `pending`, `running`, `succeeded`, `dead`, `cancelled`. |

Legal changes: `pending → running|cancelled`, `running → succeeded|pending|dead`, `dead → pending`. A running row must hold a lease. The database rejects anything else.

## Tests

```bash
make test
```

Integration tests start Postgres 17 with testcontainers. Unit tests cover validation, the transition table, and cursors.

## Related work

[VoxForge](https://github.com/Brohammad/VoxForge), [BugSathi](https://github.com/Brohammad/BugSathi), [SignalMine](https://github.com/Brohammad/PainMiningEngine).

## License

[MIT](LICENSE)
