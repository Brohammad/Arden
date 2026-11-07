# ADR 001: Postgres is the source of truth and the coordinator

## Status

Accepted

## Context

Arden has to let several workers claim work without running the same job at the same time, and it has to recover that work after a crash. A separate broker would add another failure domain before the job records exist.

## Decision

PostgreSQL stores the job and coordinates workers. Claiming will use `SELECT … FOR UPDATE SKIP LOCKED` inside a transaction that sets the lease and increments the fencing token. The claim index is `(queue, priority DESC, scheduled_at, id)` where `status = 'pending'`. Expired work is found by `lease_expires_at` where `status = 'running'`.

Redis, Kafka, and a custom consensus protocol are out of scope.

## Consequences

Correctness depends on transactional updates and constraints in this database. Throughput is bounded by Postgres lock and index contention, which Week 8 will measure before any broker is considered. Workers must treat the database as the authority on who owns a job.
