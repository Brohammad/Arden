CREATE TYPE job_status AS ENUM (
    'pending',
    'running',
    'succeeded',
    'dead',
    'cancelled'
);

CREATE TYPE attempt_status AS ENUM (
    'running',
    'succeeded',
    'failed',
    'expired',
    'cancelled'
);

CREATE TABLE workers (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    hostname          TEXT NOT NULL,
    queues            TEXT[] NOT NULL DEFAULT ARRAY['default']::text[],
    started_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_heartbeat_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    stopped_at        TIMESTAMPTZ
);

CREATE TABLE job_schedules (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    queue        TEXT NOT NULL DEFAULT 'default',
    payload      JSONB NOT NULL,
    cron_expr    TEXT NOT NULL,
    timezone     TEXT NOT NULL DEFAULT 'UTC',
    priority     SMALLINT NOT NULL DEFAULT 0,
    max_attempts INT NOT NULL DEFAULT 5,
    next_run_at  TIMESTAMPTZ NOT NULL,
    enabled      BOOLEAN NOT NULL DEFAULT TRUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT job_schedules_queue_format CHECK (queue ~ '^[a-z0-9][a-z0-9_-]{0,63}$'),
    CONSTRAINT job_schedules_payload_object CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT job_schedules_max_attempts_range CHECK (max_attempts BETWEEN 1 AND 100)
);

CREATE INDEX job_schedules_due_idx
    ON job_schedules (next_run_at)
    WHERE enabled;

CREATE TABLE jobs (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    queue            TEXT NOT NULL DEFAULT 'default',
    payload          JSONB NOT NULL,
    status           job_status NOT NULL DEFAULT 'pending',
    priority         SMALLINT NOT NULL DEFAULT 0,
    scheduled_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    attempt          INT NOT NULL DEFAULT 0,
    max_attempts     INT NOT NULL DEFAULT 5,
    lease_owner      UUID REFERENCES workers (id),
    lease_expires_at TIMESTAMPTZ,
    fencing_token    BIGINT NOT NULL DEFAULT 0,
    idempotency_key  TEXT,
    last_error       TEXT,
    result           JSONB,
    schedule_id      UUID REFERENCES job_schedules (id),
    occurrence_at    TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT jobs_queue_format CHECK (queue ~ '^[a-z0-9][a-z0-9_-]{0,63}$'),
    CONSTRAINT jobs_payload_object CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT jobs_max_attempts_range CHECK (max_attempts BETWEEN 1 AND 100),
    CONSTRAINT jobs_attempt_nonnegative CHECK (attempt >= 0),
    CONSTRAINT jobs_fencing_nonnegative CHECK (fencing_token >= 0),
    CONSTRAINT jobs_idempotency_key_len CHECK (
        idempotency_key IS NULL OR char_length(idempotency_key) BETWEEN 1 AND 255
    ),
    CONSTRAINT jobs_schedule_occurrence_pair CHECK (
        (schedule_id IS NULL AND occurrence_at IS NULL)
        OR (schedule_id IS NOT NULL AND occurrence_at IS NOT NULL)
    ),
    CONSTRAINT jobs_lease_consistency CHECK (
        (
            status = 'running'
            AND lease_owner IS NOT NULL
            AND lease_expires_at IS NOT NULL
        )
        OR (
            status <> 'running'
            AND lease_owner IS NULL
            AND lease_expires_at IS NULL
        )
    )
);

-- NULL keys stay distinct so submissions without a key are never deduplicated.
CREATE UNIQUE INDEX jobs_idempotency_key_uidx
    ON jobs (idempotency_key)
    WHERE idempotency_key IS NOT NULL;

-- One row per schedule occurrence. Overlapping occurrences are distinct jobs.
CREATE UNIQUE INDEX jobs_schedule_occurrence_uidx
    ON jobs (schedule_id, occurrence_at)
    WHERE schedule_id IS NOT NULL;

-- Matches the claim query: pending, due, highest priority, then earliest schedule.
CREATE INDEX jobs_claimable_idx
    ON jobs (queue, priority DESC, scheduled_at ASC, id ASC)
    WHERE status = 'pending';

CREATE INDEX jobs_expired_lease_idx
    ON jobs (lease_expires_at)
    WHERE status = 'running';

CREATE INDEX jobs_created_idx
    ON jobs (created_at DESC, id DESC);

CREATE TABLE job_attempts (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id         UUID NOT NULL REFERENCES jobs (id),
    worker_id      UUID REFERENCES workers (id),
    fencing_token  BIGINT NOT NULL,
    cycle_attempt  INT NOT NULL,
    status         attempt_status NOT NULL,
    error          TEXT,
    result         JSONB,
    started_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at    TIMESTAMPTZ,
    CONSTRAINT job_attempts_cycle_attempt_positive CHECK (cycle_attempt > 0),
    CONSTRAINT job_attempts_fencing_nonnegative CHECK (fencing_token >= 0),
    UNIQUE (job_id, fencing_token)
);

CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$;

CREATE TRIGGER job_schedules_before_update_timestamp
    BEFORE UPDATE ON job_schedules
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();

CREATE OR REPLACE FUNCTION jobs_enforce_insert() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.status <> 'pending'
        OR NEW.attempt <> 0
        OR NEW.fencing_token <> 0
        OR NEW.lease_owner IS NOT NULL
        OR NEW.lease_expires_at IS NOT NULL
    THEN
        RAISE EXCEPTION 'new jobs must be pending with no lease, attempt 0, and fencing token 0'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER jobs_before_insert_guard
    BEFORE INSERT ON jobs
    FOR EACH ROW
    EXECUTE FUNCTION jobs_enforce_insert();

CREATE OR REPLACE FUNCTION jobs_enforce_transition() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.status = NEW.status THEN
        RETURN NEW;
    END IF;

    IF OLD.status = 'pending' AND NEW.status = 'cancelled' THEN
        RETURN NEW;
    END IF;

    IF OLD.status = 'pending' AND NEW.status = 'running' THEN
        IF NEW.fencing_token <> OLD.fencing_token + 1 THEN
            RAISE EXCEPTION 'fencing token must increase by 1 on claim'
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW.attempt <> OLD.attempt + 1 THEN
            RAISE EXCEPTION 'attempt must increment by 1 on claim'
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END IF;

    IF OLD.status = 'running' AND NEW.status = 'pending' THEN
        IF OLD.attempt >= OLD.max_attempts THEN
            RAISE EXCEPTION 'attempts exhausted'
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW.fencing_token <> OLD.fencing_token OR NEW.attempt <> OLD.attempt THEN
            RAISE EXCEPTION 'retry must keep the fencing token and attempt'
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END IF;

    IF OLD.status = 'running' AND NEW.status IN ('succeeded', 'dead') THEN
        IF NEW.fencing_token <> OLD.fencing_token OR NEW.attempt <> OLD.attempt THEN
            RAISE EXCEPTION 'completion must keep the fencing token and attempt'
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END IF;

    IF OLD.status = 'dead' AND NEW.status = 'pending' THEN
        IF NEW.attempt <> 0 THEN
            RAISE EXCEPTION 'replay must reset attempt to 0'
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW.fencing_token <> OLD.fencing_token THEN
            RAISE EXCEPTION 'replay must keep the fencing token'
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END IF;

    RAISE EXCEPTION 'invalid job status transition: % -> %', OLD.status, NEW.status
        USING ERRCODE = 'check_violation';
END;
$$;

CREATE TRIGGER jobs_before_update_guard
    BEFORE UPDATE ON jobs
    FOR EACH ROW
    EXECUTE FUNCTION jobs_enforce_transition();

CREATE TRIGGER jobs_before_update_timestamp
    BEFORE UPDATE ON jobs
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();
