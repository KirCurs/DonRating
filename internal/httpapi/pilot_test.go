package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"example.com/ratingdon/internal/feedback"
)

// Complete the original learner's stub for the expanded storage contract.
func (s *ratingStoreStub) GetRatingRequest(context.Context, string) (feedback.RatingRequest, error) {
	return feedback.RatingRequest{}, s.err
}
func (s *ratingStoreStub) GetActiveRequest(context.Context, string) (*feedback.RatingRequest, error) {
	return nil, s.err
}
func (s *ratingStoreStub) GetLatestRequest(context.Context, string) (*feedback.RatingRequest, error) {
	return nil, s.err
}
func (s *ratingStoreStub) AcknowledgeRequest(context.Context, string, string) error { return s.err }
func (s *ratingStoreStub) AnswerRequest(context.Context, string, string, int) (feedback.RatingRequest, error) {
	return feedback.RatingRequest{}, s.err
}

type pilotStore struct {
	*ratingStoreStub
	request feedback.RatingRequest
	saveErr error
}

func (s *pilotStore) SaveRatingRequest(_ context.Context, request feedback.RatingRequest) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.request = request
	return nil
}
func (s *pilotStore) GetRatingRequest(_ context.Context, id string) (feedback.RatingRequest, error) {
	if id != s.request.ID {
		return feedback.RatingRequest{}, feedback.ErrRequestNotFound
	}
	return s.request, nil
}
func (s *pilotStore) GetLatestRequest(context.Context, string) (*feedback.RatingRequest, error) {
	if s.request.ID == "" {
		return nil, nil
	}
	return &s.request, nil
}
func (s *pilotStore) GetActiveRequest(context.Context, string) (*feedback.RatingRequest, error) {
	if s.request.ID == "" || s.request.Status == feedback.StatusAnswered {
		return nil, nil
	}
	return &s.request, nil
}
func (s *pilotStore) AcknowledgeRequest(_ context.Context, id, device string) error {
	if id != s.request.ID || device != s.request.DeviceID {
		return feedback.ErrRequestNotFound
	}
	s.request.Status = feedback.StatusDisplayed
	return nil
}
func (s *pilotStore) AnswerRequest(_ context.Context, id, device string, score int) (feedback.RatingRequest, error) {
	if id != s.request.ID || device != s.request.DeviceID {
		return feedback.RatingRequest{}, feedback.ErrRequestNotFound
	}
	s.request.Status = feedback.StatusAnswered
	s.request.Score = &score
	return s.request, nil
}

func call(router http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Host = "localhost:8080"
	request.RemoteAddr = "127.0.0.1:12345"
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestPilotRequestFlow(t *testing.T) {
	store := &pilotStore{ratingStoreStub: &ratingStoreStub{}}
	router := NewRouter(store)
	if got := call(router, "POST", "/request", `{"device_id":"reception-1"}`, "").Code; got != 409 {
		t.Fatalf("offline status %d", got)
	}
	if got := call(router, "GET", "/api/tablets/reception-1/state", "", "").Code; got != 200 {
		t.Fatalf("state %d", got)
	}
	for _, body := range []string{`{`, `{"device_id":" "}`, `{"device_id":"unknown"}`, `{"device_id":"reception-1","id":"client-choice"}`, `{"device_id":"reception-1"}{}`} {
		if got := call(router, "POST", "/requests", body, "").Code; got != 400 {
			t.Errorf("body %s: status %d", body, got)
		}
	}
	response := call(router, "POST", "/request", `{"device_id":" reception-1 "}`, "")
	if response.Code != 201 {
		t.Fatalf("create %d: %s", response.Code, response.Body.String())
	}
	var request feedback.RatingRequest
	if err := json.Unmarshal(response.Body.Bytes(), &request); err != nil {
		t.Fatal(err)
	}
	if request.ID == "" || request.DeviceID != "reception-1" || request.Status != feedback.StatusPending {
		t.Fatalf("request: %+v", request)
	}
	if got := call(router, "POST", "/api/tablets/reception-1/ack", `{"id":"`+request.ID+`"}`, "").Code; got != 200 {
		t.Fatalf("ack %d", got)
	}
	response = call(router, "POST", "/api/tablets/reception-1/answer", `{"request_id":"`+request.ID+`","score":5}`, "")
	if response.Code != 200 || store.request.Status != feedback.StatusAnswered {
		t.Fatalf("answer: %d %+v", response.Code, store.request)
	}
	if got := call(router, "GET", "/api/admin/state", "", "").Code; got != 200 {
		t.Fatalf("admin %d", got)
	}
	for _, path := range []string{"/admin", "/tablet?device_id=reception-1"} {
		if got := call(router, "GET", path, "", "").Code; got != 200 {
			t.Fatalf("page %s: %d", path, got)
		}
	}
}

func TestStorageErrorsAreMappedWithoutLeakingDetails(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
	}{{feedback.ErrRequestActive, 409}, {feedback.ErrRequestExpired, 410}, {feedback.ErrRequestClosed, 409}, {feedback.ErrRequestNotFound, 404}, {feedback.ErrInvalidRequest, 400}, {errors.New("private-db-password"), 500}} {
		t.Run(tc.err.Error(), func(t *testing.T) {
			store := &pilotStore{ratingStoreStub: &ratingStoreStub{}, saveErr: tc.err}
			router := NewRouter(store)
			call(router, "GET", "/api/tablets/reception-1/state", "", "")
			response := call(router, "POST", "/requests", `{"device_id":"reception-1"}`, "")
			if response.Code != tc.status {
				t.Fatalf("status %d", response.Code)
			}
			if strings.Contains(response.Body.String(), "private-db-password") {
				t.Fatal("leaked storage detail")
			}
		})
	}
}

func TestAuthenticationAndCrossOrigin(t *testing.T) {
	store := &pilotStore{ratingStoreStub: &ratingStoreStub{}}
	router := NewRouterWithOptions(store, Options{AdminToken: "admin-secret", Devices: []Device{{ID: "one", Name: "One"}, {ID: "two", Name: "Two"}}, DeviceTokens: map[string]string{"one": "device-one", "two": "device-two"}})
	if got := call(router, "GET", "/api/admin/state", "", "").Code; got != 401 {
		t.Fatal(got)
	}
	if got := call(router, "GET", "/api/admin/state", "", "admin-secret").Code; got != 200 {
		t.Fatal(got)
	}
	if got := call(router, "GET", "/api/tablets/one/state", "", "device-two").Code; got != 401 {
		t.Fatal(got)
	}
	if got := call(router, "GET", "/api/tablets/one/state", "", "device-one").Code; got != 200 {
		t.Fatal(got)
	}
	request := httptest.NewRequest("GET", "http://localhost:8080/api/admin/state", nil)
	request.RemoteAddr = "127.0.0.1:1"
	request.Header.Set("Authorization", "Bearer admin-secret")
	request.Header.Set("Origin", "https://other.example")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 403 {
		t.Fatal(response.Code)
	}
	// Even loopback clients cannot use passwordless endpoints with an attacker Host.
	noTokens := NewRouter(store)
	request = httptest.NewRequest("GET", "http://evil.example/api/admin/state", nil)
	request.RemoteAddr = "127.0.0.1:1"
	response = httptest.NewRecorder()
	noTokens.ServeHTTP(response, request)
	if response.Code != 401 {
		t.Fatal(response.Code)
	}
}
