package feedback

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	StatusPending   = "pending"
	StatusDisplayed = "displayed"
	StatusAnswered  = "answered"
	StatusExpired   = "expired"

	DefaultRequestTTL = 30 * time.Second
)

type RatingRequest struct {
	ID         string     `json:"id"`
	DeviceID   string     `json:"device_id"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	AnsweredAt *time.Time `json:"answered_at,omitempty"`
	Score      *int       `json:"score,omitempty"`
}

func NewRatingRequest(deviceID string) RatingRequest {
	return NewRatingRequestWithTTL(deviceID, DefaultRequestTTL)
}

func NewRatingRequestWithTTL(deviceID string, ttl time.Duration) RatingRequest {
	now := time.Now().UTC()
	return RatingRequest{
		ID: uuid.NewString(), DeviceID: deviceID, Status: StatusPending,
		CreatedAt: now, ExpiresAt: now.Add(ttl),
	}
}

// Validate checks a new request before it is persisted. Store owns transitions.
func (request RatingRequest) Validate() error {
	switch {
	case strings.TrimSpace(request.ID) == "", strings.TrimSpace(request.DeviceID) == "":
		return fmt.Errorf("%w: request and device identifiers are required", ErrInvalidRequest)
	case request.Status != StatusPending:
		return fmt.Errorf("%w: a new request must be pending", ErrInvalidRequest)
	case request.CreatedAt.IsZero(), request.ExpiresAt.IsZero(), !request.ExpiresAt.After(request.CreatedAt):
		return fmt.Errorf("%w: expiration must follow creation", ErrInvalidRequest)
	case request.AnsweredAt != nil, request.Score != nil:
		return fmt.Errorf("%w: a new request cannot have an answer", ErrInvalidRequest)
	default:
		return nil
	}
}
