package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Brohammad/Arden/internal/store"
	"github.com/Brohammad/Arden/internal/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
)

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	pool, cleanup, err := testutil.StartPostgres(ctx)
	cancel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "postgres: %v\n", err)
		os.Exit(1)
	}
	testPool = pool
	code := m.Run()
	cleanup()
	os.Exit(code)
}

func TestHealth(t *testing.T) {
	srv := newServer(t)
	status, body := do(t, http.MethodGet, srv.URL+"/healthz", "", nil)
	if status != http.StatusOK || !strings.Contains(string(body), `"ok"`) {
		t.Fatalf("healthz %d %s", status, body)
	}
	status, body = do(t, http.MethodGet, srv.URL+"/readyz", "", nil)
	if status != http.StatusOK || !strings.Contains(string(body), `"ok"`) {
		t.Fatalf("readyz %d %s", status, body)
	}
}

func TestJobLifecycle(t *testing.T) {
	reset(t)
	srv := newServer(t)

	status, body := do(t, http.MethodPost, srv.URL+"/v1/jobs", `{
		"queue":"email",
		"payload":{"to":"a@example.com"},
		"priority":5,
		"max_attempts":4,
		"scheduled_at":"2026-12-01T09:00:00Z"
	}`, map[string]string{"Idempotency-Key": "welcome-1"})
	if status != http.StatusCreated {
		t.Fatalf("create %d %s", status, body)
	}
	created := decodeJob(t, body)
	if created.Queue != "email" || created.Status != "pending" || created.Priority != 5 || created.MaxAttempts != 4 {
		t.Fatalf("created = %+v", created)
	}
	if created.Attempt != 0 || created.FencingToken != 0 || created.LeaseOwner != nil {
		t.Fatalf("initial lease fields = %+v", created)
	}
	if created.IdempotencyKey == nil || *created.IdempotencyKey != "welcome-1" {
		t.Fatalf("idempotency key = %v", created.IdempotencyKey)
	}

	status, body = do(t, http.MethodGet, srv.URL+"/v1/jobs/"+created.ID, "", nil)
	if status != http.StatusOK {
		t.Fatalf("get %d %s", status, body)
	}
	if decodeJob(t, body).ID != created.ID {
		t.Fatalf("get body = %s", body)
	}

	status, body = do(t, http.MethodPost, srv.URL+"/v1/jobs", `{"queue":"reports","payload":{"name":"daily"}}`, nil)
	if status != http.StatusCreated {
		t.Fatalf("second create %d %s", status, body)
	}

	status, body = do(t, http.MethodGet, srv.URL+"/v1/jobs?queue=email&status=pending", "", nil)
	if status != http.StatusOK {
		t.Fatalf("list %d %s", status, body)
	}
	page := decodePage(t, body)
	if len(page.Jobs) != 1 || page.Jobs[0].ID != created.ID {
		t.Fatalf("filtered list = %+v", page)
	}
}

func TestIdempotency(t *testing.T) {
	reset(t)
	srv := newServer(t)
	body := `{"queue":"email","payload":{"b":1,"a":2},"priority":1,"max_attempts":5}`
	headers := map[string]string{"Idempotency-Key": "same-key"}

	status, first := do(t, http.MethodPost, srv.URL+"/v1/jobs", body, headers)
	if status != http.StatusCreated {
		t.Fatalf("first %d %s", status, first)
	}
	original := decodeJob(t, first)

	reordered := `{"max_attempts":5,"priority":1,"payload":{"a":2,"b":1},"queue":"email"}`
	status, second := do(t, http.MethodPost, srv.URL+"/v1/jobs", reordered, headers)
	if status != http.StatusOK {
		t.Fatalf("replay %d %s", status, second)
	}
	if decodeJob(t, second).ID != original.ID {
		t.Fatalf("replay id = %s, want %s", decodeJob(t, second).ID, original.ID)
	}

	status, mismatch := do(t, http.MethodPost, srv.URL+"/v1/jobs", `{"queue":"email","payload":{"a":3}}`, headers)
	if status != http.StatusConflict || !strings.Contains(string(mismatch), `"idempotency_mismatch"`) {
		t.Fatalf("mismatch %d %s", status, mismatch)
	}

	later := `{"queue":"email","payload":{"n":1},"scheduled_at":"2026-12-02T00:00:00Z"}`
	delayHeaders := map[string]string{"Idempotency-Key": "delayed"}
	status, delayed := do(t, http.MethodPost, srv.URL+"/v1/jobs", later, delayHeaders)
	if status != http.StatusCreated {
		t.Fatalf("delayed %d %s", status, delayed)
	}
	delayedID := decodeJob(t, delayed).ID
	status, replayed := do(t, http.MethodPost, srv.URL+"/v1/jobs", later, delayHeaders)
	if status != http.StatusOK || decodeJob(t, replayed).ID != delayedID {
		t.Fatalf("delayed replay %d %s", status, replayed)
	}
	otherTime := `{"queue":"email","payload":{"n":1},"scheduled_at":"2026-12-03T00:00:00Z"}`
	status, changed := do(t, http.MethodPost, srv.URL+"/v1/jobs", otherTime, delayHeaders)
	if status != http.StatusConflict {
		t.Fatalf("changed schedule %d %s", status, changed)
	}
}

func TestConcurrentIdempotency(t *testing.T) {
	reset(t)
	srv := newServer(t)
	const n = 8
	codes := make([]int, n)
	ids := make([]string, n)
	errs := make([]string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req, err := http.NewRequest(http.MethodPost, srv.URL+"/v1/jobs", strings.NewReader(`{"payload":{"n":1}}`))
			if err != nil {
				errs[i] = err.Error()
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Idempotency-Key", "race")
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				errs[i] = err.Error()
				return
			}
			defer res.Body.Close()
			payload, err := io.ReadAll(res.Body)
			if err != nil {
				errs[i] = err.Error()
				return
			}
			codes[i] = res.StatusCode
			var parsed jobJSON
			if err := json.Unmarshal(payload, &parsed); err != nil {
				errs[i] = string(payload)
				return
			}
			ids[i] = parsed.ID
		}(i)
	}
	wg.Wait()
	for i := 0; i < n; i++ {
		if errs[i] != "" {
			t.Fatalf("request %d: %s", i, errs[i])
		}
	}

	created := 0
	var id string
	for i := 0; i < n; i++ {
		switch codes[i] {
		case http.StatusCreated:
			created++
			id = ids[i]
		case http.StatusOK:
			if id == "" {
				id = ids[i]
			} else if ids[i] != id {
				t.Fatalf("id %s does not match %s", ids[i], id)
			}
		default:
			t.Fatalf("status[%d] = %d", i, codes[i])
		}
	}
	if created != 1 || id == "" {
		t.Fatalf("created = %d id = %q codes = %v", created, id, codes)
	}

	var count int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM jobs WHERE idempotency_key = 'race'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("rows = %d", count)
	}
}

func TestCancel(t *testing.T) {
	reset(t)
	srv := newServer(t)

	status, body := do(t, http.MethodPost, srv.URL+"/v1/jobs", `{"payload":{"n":1}}`, nil)
	if status != http.StatusCreated {
		t.Fatalf("create %d %s", status, body)
	}
	id := decodeJob(t, body).ID

	status, body = do(t, http.MethodPost, srv.URL+"/v1/jobs/"+id+"/cancel", "", nil)
	if status != http.StatusOK || decodeJob(t, body).Status != "cancelled" {
		t.Fatalf("cancel %d %s", status, body)
	}
	status, body = do(t, http.MethodPost, srv.URL+"/v1/jobs/"+id+"/cancel", "", nil)
	if status != http.StatusOK || decodeJob(t, body).Status != "cancelled" {
		t.Fatalf("cancel again %d %s", status, body)
	}

	status, body = do(t, http.MethodPost, srv.URL+"/v1/jobs", `{"payload":{"n":2}}`, nil)
	if status != http.StatusCreated {
		t.Fatalf("create running candidate %d %s", status, body)
	}
	runningID := decodeJob(t, body).ID
	var workerID string
	if err := testPool.QueryRow(context.Background(), `INSERT INTO workers (hostname) VALUES ('worker-b') RETURNING id::text`).Scan(&workerID); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(context.Background(), `
		UPDATE jobs
		SET status = 'running', attempt = 1, fencing_token = 1,
		    lease_owner = $2::uuid, lease_expires_at = now() + interval '1 minute'
		WHERE id = $1::uuid`, runningID, workerID); err != nil {
		t.Fatal(err)
	}
	status, body = do(t, http.MethodPost, srv.URL+"/v1/jobs/"+runningID+"/cancel", "", nil)
	if status != http.StatusConflict || !strings.Contains(string(body), `"conflict"`) {
		t.Fatalf("cancel running %d %s", status, body)
	}
}

func TestPagination(t *testing.T) {
	reset(t)
	srv := newServer(t)
	want := map[string]struct{}{}
	for i := 0; i < 3; i++ {
		status, body := do(t, http.MethodPost, srv.URL+"/v1/jobs", fmt.Sprintf(`{"payload":{"i":%d}}`, i), nil)
		if status != http.StatusCreated {
			t.Fatalf("create %d %s", status, body)
		}
		want[decodeJob(t, body).ID] = struct{}{}
		time.Sleep(2 * time.Millisecond)
	}

	seen := map[string]struct{}{}
	cursor := ""
	pages := 0
	for {
		pages++
		if pages > 5 {
			t.Fatal("pagination did not finish")
		}
		url := srv.URL + "/v1/jobs?limit=2"
		if cursor != "" {
			url += "&cursor=" + cursor
		}
		status, body := do(t, http.MethodGet, url, "", nil)
		if status != http.StatusOK {
			t.Fatalf("page %d %s", status, body)
		}
		page := decodePage(t, body)
		if len(page.Jobs) == 0 || len(page.Jobs) > 2 {
			t.Fatalf("page size = %d", len(page.Jobs))
		}
		for _, item := range page.Jobs {
			if _, ok := seen[item.ID]; ok {
				t.Fatalf("duplicate id %s", item.ID)
			}
			seen[item.ID] = struct{}{}
		}
		if page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
	}
	if len(seen) != len(want) {
		t.Fatalf("seen %d jobs, want %d", len(seen), len(want))
	}
}

func TestRejectsBadRequests(t *testing.T) {
	reset(t)
	srv := newServer(t)

	cases := []struct {
		name   string
		method string
		path   string
		body   string
		status int
		code   string
	}{
		{name: "array payload", method: http.MethodPost, path: "/v1/jobs", body: `{"payload":[]}`, status: http.StatusBadRequest, code: "invalid_argument"},
		{name: "unknown field", method: http.MethodPost, path: "/v1/jobs", body: `{"payload":{},"nope":true}`, status: http.StatusBadRequest, code: "invalid_argument"},
		{name: "bad id", method: http.MethodGet, path: "/v1/jobs/not-a-uuid", status: http.StatusBadRequest, code: "invalid_argument"},
		{name: "missing", method: http.MethodGet, path: "/v1/jobs/6f1b5c2e-9a4d-4e1f-8c3a-0b7d6e5f4a21", status: http.StatusNotFound, code: "not_found"},
		{name: "limit", method: http.MethodGet, path: "/v1/jobs?limit=201", status: http.StatusBadRequest, code: "invalid_argument"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			status, body := do(t, tt.method, srv.URL+tt.path, tt.body, nil)
			if status != tt.status || !strings.Contains(string(body), `"`+tt.code+`"`) {
				t.Fatalf("%d %s", status, body)
			}
		})
	}
}

func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	handler := NewHandler(slog.New(slog.DiscardHandler), store.New(testPool))
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func reset(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	for _, stmt := range []string{
		`DELETE FROM job_attempts`,
		`DELETE FROM jobs`,
		`DELETE FROM workers`,
	} {
		if _, err := testPool.Exec(ctx, stmt); err != nil {
			t.Fatalf("reset: %v", err)
		}
	}
}

func do(t *testing.T, method, url, body string, headers map[string]string) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != "" || method == http.MethodPost {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	payload, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, payload
}

type jobJSON struct {
	ID             string  `json:"id"`
	Queue          string  `json:"queue"`
	Status         string  `json:"status"`
	Priority       int     `json:"priority"`
	Attempt        int     `json:"attempt"`
	MaxAttempts    int     `json:"max_attempts"`
	FencingToken   int64   `json:"fencing_token"`
	LeaseOwner     *string `json:"lease_owner"`
	IdempotencyKey *string `json:"idempotency_key"`
}

type pageJSON struct {
	Jobs       []jobJSON `json:"jobs"`
	NextCursor *string   `json:"next_cursor"`
}

func decodeJob(t *testing.T, body []byte) jobJSON {
	t.Helper()
	var job jobJSON
	if err := json.Unmarshal(body, &job); err != nil {
		t.Fatalf("decode job %s: %v", body, err)
	}
	return job
}

func decodePage(t *testing.T, body []byte) pageJSON {
	t.Helper()
	var page pageJSON
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("decode page %s: %v", body, err)
	}
	return page
}
