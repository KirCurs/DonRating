package httpapi

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"example.com/ratingdon/internal/feedback"
)

type enrollmentStore struct {
	*pilotStore
	devices map[string]feedback.RegisteredDevice
}

func (s *enrollmentStore) RegisterDevice(_ context.Context, d feedback.RegisteredDevice) error {
	s.devices[d.ID] = d
	return nil
}
func (s *enrollmentStore) ListDevices(context.Context) ([]feedback.RegisteredDevice, error) {
	out := []feedback.RegisteredDevice{}
	for _, d := range s.devices {
		out = append(out, d)
	}
	return out, nil
}
func (s *enrollmentStore) GetDevice(_ context.Context, id string) (feedback.RegisteredDevice, error) {
	d, ok := s.devices[id]
	if !ok {
		return d, feedback.ErrDeviceNotFound
	}
	return d, nil
}
func TestTabletEnrollmentAndRouting(t *testing.T) {
	store := &enrollmentStore{pilotStore: &pilotStore{ratingStoreStub: &ratingStoreStub{}}, devices: map[string]feedback.RegisteredDevice{}}
	options := Options{AdminToken: "admin-secret"}
	router := NewRouterWithOptions(store, options)
	if got := call(router, "POST", "/api/admin/devices", `{"name":"Left desk"}`, "").Code; got != 401 {
		t.Fatalf("unauthenticated enrollment: %d", got)
	}
	for _, body := range []string{`{"name":" "}`, `{"name":"` + strings.Repeat("a", 101) + `"}`, `{"name":"Desk","token":"chosen"}`} {
		if got := call(router, "POST", "/api/admin/devices", body, "admin-secret").Code; got != 400 {
			t.Fatalf("invalid enrollment %d", got)
		}
	}
	response := call(router, "POST", "/api/admin/devices", `{"name":"Left desk"}`, "admin-secret")
	if response.Code != 201 {
		t.Fatalf("enroll: %d %s", response.Code, response.Body.String())
	}
	var created struct {
		ID  string `json:"id"`
		URL string `json:"tablet_url"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(created.URL)
	if err != nil {
		t.Fatal(err)
	}
	fragment, _ := url.ParseQuery(u.Fragment)
	key := fragment.Get("token")
	if len(key) != 64 || created.ID == "" {
		t.Fatal("missing credentials")
	}
	saved := store.devices[created.ID]
	if saved.TokenHash == key || len(saved.TokenHash) != 64 {
		t.Fatal("token must be stored as a digest")
	}
	// Recreate the router to simulate restarting the application with persisted storage.
	router = NewRouterWithOptions(store, options)
	path := "/api/tablets/" + created.ID + "/state"
	for _, bad := range []string{"", "admin-secret", "wrong-device-key"} {
		if got := call(router, "GET", path, "", bad).Code; got != 401 {
			t.Fatalf("bad tablet key accepted: %d", got)
		}
	}
	if got := call(router, "GET", path, "", key).Code; got != 200 {
		t.Fatalf("tablet status %d", got)
	}
	state := call(router, "GET", "/api/admin/state", "", "admin-secret")
	if state.Code != 200 || !strings.Contains(state.Body.String(), created.ID) || strings.Contains(state.Body.String(), key) || strings.Contains(state.Body.String(), saved.TokenHash) {
		t.Fatalf("admin state %s", state.Body.String())
	}
	response = call(router, "POST", "/requests", `{"device_id":"`+created.ID+`"}`, "admin-secret")
	if response.Code != 201 || store.request.DeviceID != created.ID {
		t.Fatalf("targeted request %d %s", response.Code, response.Body.String())
	}
	if got := call(router, "GET", "/api/tablets/unknown/state", "", key).Code; got != 404 {
		t.Fatalf("unknown tablet %d", got)
	}
}
