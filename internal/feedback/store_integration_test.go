package feedback

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Integration tests only use an explicitly supplied test database. Every test
// creates and later removes its own schema; existing tables are never touched.
func integrationStore(t *testing.T) (*Store, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "feedback_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	config.MaxConns = 16
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		if _, err := admin.Exec(context.Background(), "DROP SCHEMA "+quotedSchema+" CASCADE"); err != nil {
			t.Errorf("remove test schema: %v", err)
		}
		admin.Close()
	})
	for _, name := range []string{"000001_create_ratings.up.sql", "000002_create_rating_requests.up.sql"} {
		applyTestMigration(t, pool, name)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO ratings VALUES ('orphan-legacy', 4), ('answered-legacy', 5);
		INSERT INTO rating_requests VALUES ('expired-legacy', 'legacy-device'), ('answered-legacy', 'legacy-device')`); err != nil {
		t.Fatal(err)
	}
	applyTestMigration(t, pool, "000003_request_lifecycle.up.sql")
	return NewStore(pool), pool
}

func applyTestMigration(t *testing.T, pool *pgxpool.Pool, name string) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("..", "..", "migrations", name))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), string(content)); err != nil {
		t.Fatalf("migration %s: %v", name, err)
	}
}

func saveTestRequest(t *testing.T, store *Store, device string) RatingRequest {
	t.Helper()
	request := NewRatingRequestWithTTL(device, time.Minute)
	if err := store.SaveRatingRequest(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	return request
}

func forceExpiry(t *testing.T, pool *pgxpool.Pool, requestID string) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `UPDATE rating_requests SET created_at=clock_timestamp()-INTERVAL '2 seconds',
		expires_at=clock_timestamp()-INTERVAL '1 second' WHERE id=$1`, requestID)
	if err != nil {
		t.Fatal(err)
	}
}

func TestStoreLifecycleIntegration(t *testing.T) {
	store, pool := integrationStore(t)
	ctx := context.Background()
	request := saveTestRequest(t, store, "terminal-1")
	if err := store.SaveRatingRequest(ctx, NewRatingRequest("terminal-1")); !errors.Is(err, ErrRequestActive) {
		t.Fatalf("second active request: %v", err)
	}
	if err := store.AcknowledgeRequest(ctx, request.ID, "other-terminal"); !errors.Is(err, ErrRequestNotFound) {
		t.Fatalf("wrong-device acknowledgement: %v", err)
	}
	if _, err := store.AnswerRequest(ctx, request.ID, "other-terminal", 5); !errors.Is(err, ErrRequestNotFound) {
		t.Fatalf("wrong-device answer: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := store.AcknowledgeRequest(ctx, request.ID, request.DeviceID); err != nil {
			t.Fatalf("acknowledgement %d: %v", i, err)
		}
	}
	active, err := store.GetActiveRequest(ctx, request.DeviceID)
	if err != nil || active == nil || active.Status != StatusDisplayed {
		t.Fatalf("displayed request: %+v, %v", active, err)
	}
	// A new Store instance simulates a backend restart: state resides in SQL.
	restarted := NewStore(pool)
	answer, err := restarted.AnswerRequest(ctx, request.ID, request.DeviceID, 5)
	if err != nil || answer.Status != StatusAnswered || answer.Score == nil || *answer.Score != 5 || answer.AnsweredAt == nil {
		t.Fatalf("answer: %+v, %v", answer, err)
	}
	retry, err := restarted.AnswerRequest(ctx, request.ID, request.DeviceID, 5)
	if err != nil || retry.AnsweredAt == nil || !retry.AnsweredAt.Equal(*answer.AnsweredAt) {
		t.Fatalf("idempotent retry: %+v, %v", retry, err)
	}
	if _, err := restarted.AnswerRequest(ctx, request.ID, request.DeviceID, 4); !errors.Is(err, ErrRatingAlreadyExists) {
		t.Fatalf("changed answer accepted: %v", err)
	}
	if err := restarted.AcknowledgeRequest(ctx, request.ID, request.DeviceID); !errors.Is(err, ErrRequestClosed) {
		t.Fatalf("closed acknowledgement: %v", err)
	}
	active, err = restarted.GetActiveRequest(ctx, request.DeviceID)
	if err != nil || active != nil {
		t.Fatalf("answered request still active: %+v, %v", active, err)
	}
	latest, err := restarted.GetLatestRequest(ctx, request.DeviceID)
	if err != nil || latest == nil || latest.Status != StatusAnswered || latest.Score == nil || *latest.Score != 5 {
		t.Fatalf("latest result: %+v, %v", latest, err)
	}
	_ = saveTestRequest(t, restarted, request.DeviceID)
	var count int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM ratings WHERE request_id=$1", request.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("expected one persisted rating, got %d: %v", count, err)
	}
}

func TestStoreExpiryIntegration(t *testing.T) {
	store, pool := integrationStore(t)
	ctx := context.Background()
	for _, action := range []string{"acknowledge", "answer", "poll", "create"} {
		t.Run(action, func(t *testing.T) {
			request := saveTestRequest(t, store, "expiry-"+action)
			forceExpiry(t, pool, request.ID)
			switch action {
			case "acknowledge":
				if err := store.AcknowledgeRequest(ctx, request.ID, request.DeviceID); !errors.Is(err, ErrRequestExpired) {
					t.Fatalf("expired acknowledgement: %v", err)
				}
			case "answer":
				if _, err := store.AnswerRequest(ctx, request.ID, request.DeviceID, 5); !errors.Is(err, ErrRequestExpired) {
					t.Fatalf("expired answer: %v", err)
				}
			case "poll":
				if active, err := store.GetActiveRequest(ctx, request.DeviceID); err != nil || active != nil {
					t.Fatalf("expired poll: %+v, %v", active, err)
				}
			case "create":
				_ = saveTestRequest(t, store, request.DeviceID)
			}
			// Inspect SQL directly before another helper can expire the record.
			var status string
			if err := pool.QueryRow(ctx, "SELECT status FROM rating_requests WHERE id=$1", request.ID).Scan(&status); err != nil || status != StatusExpired {
				t.Fatalf("expiration was not committed: %q, %v", status, err)
			}
			if _, err := store.GetRating(ctx, request.ID); !errors.Is(err, ErrRatingNotFound) {
				t.Fatalf("expired request produced a rating: %v", err)
			}
		})
	}
}

func TestStoreConcurrentCreateIntegration(t *testing.T) {
	store, _ := integrationStore(t)
	const workers = 12
	start := make(chan struct{})
	results := make(chan error, workers)
	var group sync.WaitGroup
	for i := 0; i < workers; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			results <- store.SaveRatingRequest(context.Background(), NewRatingRequest("concurrent-device"))
		}()
	}
	close(start)
	group.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrRequestActive) {
			t.Fatalf("unexpected create error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("expected one active request, got %d", successes)
	}
}

func TestStoreConcurrentAnswerIntegration(t *testing.T) {
	store, pool := integrationStore(t)
	ctx := context.Background()
	for _, mixed := range []bool{false, true} {
		request := saveTestRequest(t, store, uuid.NewString())
		const workers = 12
		start := make(chan struct{})
		type result struct {
			answer RatingRequest
			err    error
		}
		results := make(chan result, workers)
		var group sync.WaitGroup
		for i := 0; i < workers; i++ {
			score := 5
			if mixed && i%2 == 0 {
				score = 4
			}
			group.Add(1)
			go func(score int) {
				defer group.Done()
				<-start
				answer, err := store.AnswerRequest(ctx, request.ID, request.DeviceID, score)
				results <- result{answer, err}
			}(score)
		}
		close(start)
		group.Wait()
		close(results)
		successes, conflicts := 0, 0
		var committedScore int
		for result := range results {
			if result.err == nil {
				successes++
				if result.answer.Score == nil {
					t.Fatal("successful answer has no score")
				}
				if committedScore != 0 && committedScore != *result.answer.Score {
					t.Fatal("two different scores were reported successful")
				}
				committedScore = *result.answer.Score
			} else if errors.Is(result.err, ErrRatingAlreadyExists) {
				conflicts++
			} else {
				t.Fatalf("unexpected answer error: %v", result.err)
			}
		}
		if !mixed && (successes != workers || conflicts != 0) {
			t.Fatalf("same-score retries: successes=%d conflicts=%d", successes, conflicts)
		}
		if mixed && (successes != workers/2 || conflicts != workers/2) {
			t.Fatalf("mixed-score retries: successes=%d conflicts=%d", successes, conflicts)
		}
		var count int
		if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM ratings WHERE request_id=$1", request.ID).Scan(&count); err != nil || count != 1 {
			t.Fatalf("concurrent answers persisted %d ratings: %v", count, err)
		}
	}
}

func TestStoreMigrationPreservesLegacyIntegration(t *testing.T) {
	store, pool := integrationStore(t)
	ctx := context.Background()
	orphan, err := store.GetRating(ctx, "orphan-legacy")
	if err != nil || orphan.Score != 4 {
		t.Fatalf("legacy orphan lost: %+v, %v", orphan, err)
	}
	expired, err := store.GetRatingRequest(ctx, "expired-legacy")
	if err != nil || expired.Status != StatusExpired {
		t.Fatalf("legacy request reactivated: %+v, %v", expired, err)
	}
	answered, err := store.GetRatingRequest(ctx, "answered-legacy")
	if err != nil || answered.Status != StatusAnswered || answered.Score == nil || *answered.Score != 5 {
		t.Fatalf("legacy answer lost: %+v, %v", answered, err)
	}
	applyTestMigration(t, pool, "000003_request_lifecycle.down.sql")
	var ratingCount, requestCount int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM ratings").Scan(&ratingCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM rating_requests").Scan(&requestCount); err != nil {
		t.Fatal(err)
	}
	if ratingCount != 2 || requestCount != 2 {
		t.Fatalf("down migration removed data: %d ratings, %d requests", ratingCount, requestCount)
	}
}
