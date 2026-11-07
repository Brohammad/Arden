DROP TRIGGER IF EXISTS jobs_before_update_timestamp ON jobs;
DROP TRIGGER IF EXISTS jobs_before_update_guard ON jobs;
DROP TRIGGER IF EXISTS jobs_before_insert_guard ON jobs;
DROP TRIGGER IF EXISTS job_schedules_before_update_timestamp ON job_schedules;

DROP FUNCTION IF EXISTS jobs_enforce_transition();
DROP FUNCTION IF EXISTS jobs_enforce_insert();
DROP FUNCTION IF EXISTS set_updated_at();

DROP TABLE IF EXISTS job_attempts;
DROP TABLE IF EXISTS jobs;
DROP TABLE IF EXISTS job_schedules;
DROP TABLE IF EXISTS workers;

DROP TYPE IF EXISTS attempt_status;
DROP TYPE IF EXISTS job_status;
