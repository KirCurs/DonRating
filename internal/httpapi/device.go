package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"example.com/ratingdon/internal/feedback"
	"github.com/google/uuid"
)

// DeviceRegistry persists enrollment independently of request storage.
type DeviceRegistry interface {
	RegisterDevice(context.Context, feedback.RegisteredDevice) error
	ListDevices(context.Context) ([]feedback.RegisteredDevice, error)
	GetDevice(context.Context, string) (feedback.RegisteredDevice, error)
}

func (h *Handler) configuredDevice(id string) (Device, bool) {
	for _, d := range h.runtime.options.Devices {
		if d.ID == id {
			return d, true
		}
	}
	return Device{}, false
}
func (h *Handler) devices(ctx context.Context) ([]Device, error) {
	devices := append([]Device{}, h.runtime.options.Devices...)
	if h.registry == nil {
		return devices, nil
	}
	registered, err := h.registry.ListDevices(ctx)
	if err != nil {
		return nil, err
	}
	for _, d := range registered {
		if _, exists := h.configuredDevice(d.ID); !exists {
			devices = append(devices, Device{ID: d.ID, Name: d.Name})
		}
	}
	return devices, nil
}
func (h *Handler) deviceExists(ctx context.Context, id string) (bool, error) {
	if _, ok := h.configuredDevice(id); ok {
		return true, nil
	}
	if h.registry == nil {
		return false, nil
	}
	_, err := h.registry.GetDevice(ctx, id)
	if errors.Is(err, feedback.ErrDeviceNotFound) {
		return false, nil
	}
	return err == nil, err
}
func (h *Handler) RegisterDevice(w http.ResponseWriter, r *http.Request) {
	if h.registry == nil {
		fail(w, 503, "registration_unavailable", "Регистрация планшетов недоступна")
		return
	}
	var input struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || utf8.RuneCountInString(input.Name) > 100 {
		fail(w, 400, "invalid_device", "Укажите название планшета до 100 символов")
		return
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		fail(w, 500, "registration_failed", "Не удалось создать ключ планшета")
		return
	}
	token := hex.EncodeToString(key)
	id := "tablet-" + uuid.NewString()
	// Avoid collisions with explicitly configured IDs as well as database IDs.
	if _, exists := h.configuredDevice(id); exists {
		fail(w, 409, "device_conflict", "Повторите добавление планшета")
		return
	}
	digest := sha256.Sum256([]byte(token))
	device := feedback.RegisteredDevice{ID: id, Name: input.Name, TokenHash: hex.EncodeToString(digest[:])}
	if err := h.registry.RegisterDevice(r.Context(), device); err != nil {
		storageError(w, err)
		return
	}
	writeJSON(w, 201, map[string]string{"id": id, "name": input.Name, "tablet_url": "/tablet?device_id=" + url.QueryEscape(id) + "#token=" + token})
}
func hashedTokenMatches(r *http.Request, hash string) bool {
	raw := r.Header.Get("Authorization")
	if !strings.HasPrefix(raw, "Bearer ") {
		return false
	}
	sum := sha256.Sum256([]byte(strings.TrimPrefix(raw, "Bearer ")))
	return subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sum[:])), []byte(hash)) == 1
}
