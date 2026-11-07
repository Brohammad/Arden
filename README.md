# Arden

**Status: design / not implemented** — do not treat this repository as production software until tagged releases and tests exist.

Distributed job scheduler and execution engine in Go. Intended to demonstrate Postgres-backed scheduling, worker leasing, fencing tokens, crash recovery, retries, delayed jobs, DLQ, fair scheduling, observability, fault injection, and benchmarks.

**Portfolio:** [Upcoming on brohammad.tech](https://brohammad.tech/#projects)

## Planned scope (subject to change)

| Area | Intent |
|------|--------|
| Scheduler | Postgres as source of truth; job leasing with fencing tokens |
| Workers | Heartbeats, crash recovery, at-least-once execution with idempotency |
| Policies | Retry, backoff, delayed and recurring jobs, dead-letter queue |
| Operations | OpenTelemetry metrics/traces, admin visibility |
| Hardening | Fault injection suite and performance benchmarks |

## Repository state

- No scheduler implementation in this repo yet.
- Design notes will land here before code claims production behavior.

## Related work

Author's shipped systems: [VoxForge](https://github.com/Brohammad/VoxForge), [BugSathi](https://github.com/Brohammad/BugSathi), [SignalMine](https://github.com/Brohammad/PainMiningEngine).

## License

TBD — MIT intended once code lands.
