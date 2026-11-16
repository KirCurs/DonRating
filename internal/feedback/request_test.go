package feedback

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNewRatingRequest(t *testing.T) {
	before := time.Now()
	request := NewRatingRequest("device-1")
	if _, err := uuid.Parse(request.ID); err != nil {
		t.Fatalf("request ID is not a UUID: %v", err)
	}
	if request.DeviceID != "device-1" || request.Status != StatusPending {
		t.Fatalf("unexpected request: %+v", request)
	}
	if request.CreatedAt.Before(before) || request.ExpiresAt.Sub(request.CreatedAt) != 30*time.Second {
		t.Fatalf("invalid request lifetime: %+v", request)
	}
	if err := request.Validate(); err != nil {
		t.Fatalf("new request is invalid: %v", err)
	}
	if other := NewRatingRequest("device-1"); other.ID == request.ID {
		t.Fatal("constructors reused an identifier")
	}
}

func TestValidateNewRequest(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*RatingRequest)
	}{
		{"blank_id", func(r *RatingRequest) { r.ID = "  " }},
		{"blank_device", func(r *RatingRequest) { r.DeviceID = "\t" }},
		{"unknown_state", func(r *RatingRequest) { r.Status = "unknown" }},
		{"answered_state", func(r *RatingRequest) { r.Status = StatusAnswered }},
		{"displayed_state", func(r *RatingRequest) { r.Status = StatusDisplayed }},
		{"zero_created", func(r *RatingRequest) { r.CreatedAt = time.Time{} }},
		{"zero_expiry", func(r *RatingRequest) { r.ExpiresAt = time.Time{} }},
		{"equal_expiry", func(r *RatingRequest) { r.ExpiresAt = r.CreatedAt }},
		{"negative_lifetime", func(r *RatingRequest) { r.ExpiresAt = r.CreatedAt.Add(-time.Second) }},
		{"prepopulated_score", func(r *RatingRequest) { n := 5; r.Score = &n }},
		{"prepopulated_answer", func(r *RatingRequest) { now := time.Now(); r.AnsweredAt = &now }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			request := NewRatingRequest("device-1")
			tc.mutate(&request)
			if err := request.Validate(); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("expected invalid request, got %v", err)
			}
		})
	}
	if err := NewRatingRequestWithTTL("device-1", 0).Validate(); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("zero TTL accepted: %v", err)
	}
}
