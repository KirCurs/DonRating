package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"example.com/ratingdon/internal/feedback"
)

type ratingStoreStub struct {
	rating feedback.Rating
	err    error
}

func (stub *ratingStoreStub) SaveRating(ctx context.Context, rating feedback.Rating) error {
	return stub.err
}

func (stub *ratingStoreStub) SaveRatingRequest(ctx context.Context, request feedback.RatingRequest) error {
	return stub.err
}

func (stub *ratingStoreStub) GetRating(ctx context.Context, requestID string) (feedback.Rating, error) {
	return stub.rating, stub.err
}

func TestGetRatingSuccess(t *testing.T) {
	stub := &ratingStoreStub{
		rating: feedback.Rating{
			RequestID: "request-1",
			Score:     5,
		},
	}

	router := NewRouter(stub)
	req := httptest.NewRequest(http.MethodGet, "/rating/request-1", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Host = "localhost:8080"
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != 200 {
		t.Errorf("Ожидали 200, получили: %d", recorder.Code)
	}
	var rating feedback.Rating
	decoder := json.NewDecoder(recorder.Body)
	err := decoder.Decode(&rating)

	if err != nil {
		t.Fatalf("Не удалось прочитать json: %v ", err)
	}

	if rating.RequestID != stub.rating.RequestID || rating.Score != stub.rating.Score {
		t.Errorf("Полученая оценка не совпадает с исходной: ожидали %v, получили %v", stub.rating, rating)
	}

}
