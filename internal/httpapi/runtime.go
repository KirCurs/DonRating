package httpapi

import (
	"context"
	"crypto/subtle"
	"errors"
	"example.com/ratingdon/internal/feedback"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Device struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type Options struct {
	Devices      []Device
	RequestTTL   time.Duration
	AdminToken   string
	DeviceTokens map[string]string
	Slideshow    http.Handler
	Ready        func(context.Context) error
}
type runtimeState struct {
	options Options
	mu      sync.Mutex
	seen    map[string]time.Time
}

func (s *runtimeState) knownDevice(id string) bool {
	for _, d := range s.options.Devices {
		if d.ID == id {
			return true
		}
	}
	return false
}
func (s *runtimeState) touch(id string, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seen[id] = now
}
func (s *runtimeState) lastSeen(id string) *time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.seen[id]
	if !ok {
		return nil
	}
	return &t
}
func (s *runtimeState) online(id string, now time.Time) bool {
	t := s.lastSeen(id)
	return t != nil && now.Sub(*t) < 8*time.Second
}
func localRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	hostname := r.Host
	if h, _, err := net.SplitHostPort(hostname); err == nil {
		hostname = h
	}
	return ip != nil && ip.IsLoopback() && (hostname == "localhost" || hostname == "127.0.0.1" || hostname == "::1")
}
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host == r.Host
}
func tokenMatches(r *http.Request, token string) bool {
	raw := r.Header.Get("Authorization")
	if !strings.HasPrefix(raw, "Bearer ") {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(raw, "Bearer ")), []byte(token)) == 1
}
func (h *Handler) admin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(r) {
			fail(w, 403, "origin_denied", "Недопустимый источник запроса")
			return
		}
		token := h.runtime.options.AdminToken
		if (token != "" && !tokenMatches(r, token)) || (token == "" && !localRequest(r)) {
			fail(w, 401, "unauthorized", "Введите ключ администратора")
			return
		}
		next(w, r)
	}
}
func (h *Handler) tablet(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("device_id")

		if !sameOrigin(r) {
			fail(w, 403, "origin_denied", "Недопустимый источник запроса")
			return
		}
		if _, configured := h.configuredDevice(id); configured {
			token := h.runtime.options.DeviceTokens[id]
			if (token != "" && !tokenMatches(r, token)) || (token == "" && !localRequest(r)) {
				fail(w, 401, "unauthorized", "Укажите ключ этого планшета")
				return
			}
		} else {
			if h.registry == nil {
				fail(w, 404, "unknown_device", "Планшет не зарегистрирован")
				return
			}
			device, err := h.registry.GetDevice(r.Context(), id)
			if errors.Is(err, feedback.ErrDeviceNotFound) {
				fail(w, 404, "unknown_device", "Планшет не зарегистрирован")
				return
			}
			if err != nil {
				storageError(w, err)
				return
			}
			if !hashedTokenMatches(r, device.TokenHash) {
				fail(w, 401, "unauthorized", "Укажите ключ этого планшета")
				return
			}
		}
		next(w, r)
	}
}
