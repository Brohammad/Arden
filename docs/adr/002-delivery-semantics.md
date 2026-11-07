# ADR 002: At-least-once delivery, fencing, and idempotency

## Status

Accepted

## Context

A worker can run a side effect and then crash before it records success. Another worker can pick the job up. The first worker can also resume after its lease has expired and try to finish work it no longer owns.

## Decision

Arden promises at-least-once execution, not exactly-once execution.

- `fencing_token` starts at 0 and increases by 1 on every claim. It never decreases, including when a dead job is replayed.
- Completion and lease renewal must match the current token and lease owner. A stale worker's write is rejected.
- Handlers that call another system send the job id as a stable idempotency key and include the fencing token. Checking only the job row does not stop a stale process from repeating an external side effect.
- Clients deduplicate submission with `Idempotency-Key`. The same key and the same queue, payload, priority, and max attempts return the original job. `scheduled_at` is part of that comparison only when the client sent it. A different body with the same key is a conflict.
- Each recurring occurrence is its own job, unique on `(schedule_id, occurrence_at)`. Overlap is allowed. After downtime the scheduler arms the next future occurrence and does not replay missed ticks.

Week 1 stores these columns and enforces the token rules in the database. Workers and the schedule poller are later weeks.

## Consequences

Callers still need idempotent side effects. A handler that cannot accept an idempotency key or a fencing token can run twice. Replaying a dead job does not reuse old attempt rows, because history is unique on `(job_id, fencing_token)` and the token keeps climbing.
