package feedback

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrRatingAlreadyExists = errors.New("Оценка уже существует")
	ErrRatingNotFound      = errors.New("Оценка не найдена")
	ErrRequestActive       = errors.New("an active request already exists for this device")
	ErrRequestNotFound     = errors.New("rating request not found")
	ErrRequestExpired      = errors.New("rating request expired")
	ErrRequestClosed       = errors.New("rating request is closed")
	ErrInvalidRequest      = errors.New("invalid rating request")
)

type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

// SaveRating is retained for legacy callers. New device submissions must use
// AnswerRequest to enforce ownership, expiration and the request lifecycle.
func (store *Store) SaveRating(ctx context.Context, rating Rating) error {
	if err := rating.Validate(); err != nil {
		return err
	}
	_, err := store.db.Exec(ctx, "INSERT INTO ratings (request_id, score) VALUES ($1, $2)", rating.RequestID, rating.Score)
	if isUniqueViolation(err) {
		return ErrRatingAlreadyExists
	}
	return err
}

func (store *Store) GetRating(ctx context.Context, requestID string) (Rating, error) {
	var rating Rating
	err := store.db.QueryRow(ctx, "SELECT request_id, score FROM ratings WHERE request_id=$1", requestID).Scan(&rating.RequestID, &rating.Score)
	if errors.Is(err, pgx.ErrNoRows) {
		return Rating{}, ErrRatingNotFound
	}
	return rating, err
}

func (store *Store) SaveRatingRequest(ctx context.Context, request RatingRequest) error {
	if err := request.Validate(); err != nil {
		return err
	}
	tx, err := store.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// The advisory lock also serializes the absence of a row. The partial
	// unique index remains a database-level backstop for other writers.
	if err = lockDevice(ctx, tx, request.DeviceID); err != nil {
		return err
	}
	if err = expireDeviceRequests(ctx, tx, request.DeviceID); err != nil {
		return err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
		return err
	}
	if !request.ExpiresAt.After(now) {
		return ErrRequestExpired
	}
	var active bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM rating_requests WHERE device_id=$1 AND status IN ('pending','displayed'))`, request.DeviceID).Scan(&active); err != nil {
		return err
	}
	if active {
		return ErrRequestActive
	}
	_, err = tx.Exec(ctx, `INSERT INTO rating_requests(id, device_id, status, created_at, expires_at)
		VALUES ($1,$2,$3,$4,$5)`, request.ID, request.DeviceID, request.Status, request.CreatedAt, request.ExpiresAt)
	if isUniqueViolation(err) {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "rating_requests_one_active_per_device" {
			return ErrRequestActive
		}
		return fmt.Errorf("%w: duplicate request identifier", ErrInvalidRequest)
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

const selectRequest = `SELECT rr.id, rr.device_id, rr.status, rr.created_at, rr.expires_at, rr.answered_at, r.score
	FROM rating_requests rr LEFT JOIN ratings r ON r.request_id=rr.id`

func scanRequest(row pgx.Row) (RatingRequest, error) {
	var request RatingRequest
	err := row.Scan(&request.ID, &request.DeviceID, &request.Status, &request.CreatedAt, &request.ExpiresAt, &request.AnsweredAt, &request.Score)
	if errors.Is(err, pgx.ErrNoRows) {
		return RatingRequest{}, ErrRequestNotFound
	}
	return request, err
}

func (store *Store) GetRatingRequest(ctx context.Context, requestID string) (RatingRequest, error) {
	_, err := store.db.Exec(ctx, `UPDATE rating_requests SET status='expired'
		WHERE id=$1 AND status IN ('pending','displayed') AND expires_at<=clock_timestamp()`, requestID)
	if err != nil {
		return RatingRequest{}, err
	}
	return scanRequest(store.db.QueryRow(ctx, selectRequest+" WHERE rr.id=$1", requestID))
}

func (store *Store) GetActiveRequest(ctx context.Context, deviceID string) (*RatingRequest, error) {
	return store.getDeviceRequest(ctx, deviceID, true)
}

// GetLatestRequest includes closed history for the admin's last-result display.
func (store *Store) GetLatestRequest(ctx context.Context, deviceID string) (*RatingRequest, error) {
	return store.getDeviceRequest(ctx, deviceID, false)
}

func (store *Store) getDeviceRequest(ctx context.Context, deviceID string, activeOnly bool) (*RatingRequest, error) {
	if strings.TrimSpace(deviceID) == "" {
		return nil, ErrInvalidRequest
	}
	tx, err := store.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err = lockDevice(ctx, tx, deviceID); err != nil {
		return nil, err
	}
	if err = expireDeviceRequests(ctx, tx, deviceID); err != nil {
		return nil, err
	}
	query := selectRequest + " WHERE rr.device_id=$1"
	if activeOnly {
		query += " AND rr.status IN ('pending','displayed')"
	}
	query += " ORDER BY rr.created_at DESC, rr.id DESC LIMIT 1"
	request, err := scanRequest(tx.QueryRow(ctx, query, deviceID))
	if errors.Is(err, ErrRequestNotFound) {
		return nil, tx.Commit(ctx)
	}
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &request, nil
}

func (store *Store) AcknowledgeRequest(ctx context.Context, requestID, deviceID string) error {
	tx, request, now, err := store.lockRequest(ctx, requestID, deviceID)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = expireLockedRequest(ctx, tx, request, now); err != nil {
		return err
	}
	switch request.Status {
	case StatusPending:
		if _, err = tx.Exec(ctx, "UPDATE rating_requests SET status='displayed' WHERE id=$1", requestID); err != nil {
			return err
		}
	case StatusDisplayed:
		// Retrying a successful acknowledgement is harmless.
	case StatusExpired:
		return ErrRequestExpired
	default:
		return ErrRequestClosed
	}
	return tx.Commit(ctx)
}

func (store *Store) AnswerRequest(ctx context.Context, requestID, deviceID string, score int) (RatingRequest, error) {
	if score < 1 || score > 5 {
		return RatingRequest{}, fmt.Errorf("%w: score must be between 1 and 5", ErrInvalidRequest)
	}
	tx, request, now, err := store.lockRequest(ctx, requestID, deviceID)
	if err != nil {
		return RatingRequest{}, err
	}
	defer tx.Rollback(ctx)
	// A committed answer remains retryable after its original deadline.
	if request.Status == StatusAnswered {
		if request.Score != nil && *request.Score == score {
			return request, tx.Commit(ctx)
		}
		return RatingRequest{}, ErrRatingAlreadyExists
	}
	if err = expireLockedRequest(ctx, tx, request, now); err != nil {
		return RatingRequest{}, err
	}
	if request.Status == StatusExpired {
		return RatingRequest{}, ErrRequestExpired
	}
	if request.Status != StatusPending && request.Status != StatusDisplayed {
		return RatingRequest{}, ErrRequestClosed
	}
	_, err = tx.Exec(ctx, "INSERT INTO ratings(request_id, score) VALUES ($1,$2)", requestID, score)
	if isUniqueViolation(err) {
		return RatingRequest{}, ErrRatingAlreadyExists
	}
	if err != nil {
		return RatingRequest{}, err
	}
	_, err = tx.Exec(ctx, "UPDATE rating_requests SET status='answered', answered_at=$2 WHERE id=$1", requestID, now)
	if err != nil {
		return RatingRequest{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return RatingRequest{}, err
	}
	request.Status, request.AnsweredAt, request.Score = StatusAnswered, &now, &score
	return request, nil
}

func (store *Store) lockRequest(ctx context.Context, requestID, deviceID string) (pgx.Tx, RatingRequest, time.Time, error) {
	if strings.TrimSpace(requestID) == "" || strings.TrimSpace(deviceID) == "" {
		return nil, RatingRequest{}, time.Time{}, ErrInvalidRequest
	}
	tx, err := store.db.Begin(ctx)
	if err != nil {
		return nil, RatingRequest{}, time.Time{}, err
	}
	// Lock the request before looking up its score. A joined SELECT that waits
	// for another answer can otherwise retain the older ratings snapshot.
	request, err := scanRequest(tx.QueryRow(ctx, `SELECT rr.id, rr.device_id, rr.status, rr.created_at, rr.expires_at, rr.answered_at, NULL::integer
		FROM rating_requests rr WHERE rr.id=$1 AND rr.device_id=$2 FOR UPDATE`, requestID, deviceID))
	if err != nil {
		tx.Rollback(ctx)
		return nil, RatingRequest{}, time.Time{}, err
	}
	var score int
	err = tx.QueryRow(ctx, "SELECT score FROM ratings WHERE request_id=$1", requestID).Scan(&score)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		tx.Rollback(ctx)
		return nil, RatingRequest{}, time.Time{}, err
	}
	if err == nil {
		request.Score = &score
	}
	var now time.Time
	if err = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
		tx.Rollback(ctx)
		return nil, RatingRequest{}, time.Time{}, err
	}
	return tx, request, now, nil
}

// Expiration commits even though the attempted action returns an error.
func expireLockedRequest(ctx context.Context, tx pgx.Tx, request RatingRequest, now time.Time) error {
	if (request.Status == StatusPending || request.Status == StatusDisplayed) && !request.ExpiresAt.After(now) {
		if _, err := tx.Exec(ctx, "UPDATE rating_requests SET status='expired' WHERE id=$1", request.ID); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return ErrRequestExpired
	}
	return nil
}

func lockDevice(ctx context.Context, tx pgx.Tx, deviceID string) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", deviceID)
	return err
}

func expireDeviceRequests(ctx context.Context, tx pgx.Tx, deviceID string) error {
	_, err := tx.Exec(ctx, `UPDATE rating_requests SET status='expired'
		WHERE device_id=$1 AND status IN ('pending','displayed') AND expires_at<=clock_timestamp()`, deviceID)
	return err
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
