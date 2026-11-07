# ADR 003: Job state machine

## Status

Accepted

## Context

Workers, the API, and recovery will all change job rows. Those writes have to agree on which status changes are legal, or a crash path can mark a job succeeded without a lease or cancel a job that is already running.

## Decision

The statuses are `pending`, `running`, `succeeded`, `dead`, and `cancelled`. A trigger rejects any other change. Same-status updates stay legal so a heartbeat can extend `lease_expires_at`.

- Inserts are `pending`, with `attempt = 0`, `fencing_token = 0`, and no lease.
- `pending → running` is a claim. `attempt` and `fencing_token` each increase by 1, and both lease columns are set.
- `pending → cancelled` is the only cancellation. Cancelling an already cancelled job returns the row unchanged.
- `running → succeeded` and `running → dead` clear the lease and keep the token and attempt.
- `running → pending` is a retryable failure or an expired lease. It keeps the token and attempt, clears the lease, and is rejected when `attempt >= max_attempts`.
- `dead → pending` is a replay. It sets `attempt` back to 0 and keeps the fencing token.

`attempt` counts claims in the current cycle. `max_attempts` is that cycle's budget, from 1 to 100, default 5. A running job must have a lease owner and expiry. Every other status must have neither.

## Consequences

The database, not only the Go transition table, rejects illegal writes. Week 2 claiming and Week 5 replay have to follow these rules instead of inventing new statuses. In-flight cancellation is not part of this machine.
