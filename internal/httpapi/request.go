package httpapi

import (
	"net/http"
	"strings"
	"time"

	"example.com/ratingdon/internal/feedback"
)

type createRatingRequestInput struct {
	DeviceID string `json:"device_id"`
}

func (h *Handler) CreateRatingRequest(w http.ResponseWriter, r *http.Request) {
	var input createRatingRequestInput
	if !decodeJSON(w, r, &input) {
		return
	}
	input.DeviceID = strings.TrimSpace(input.DeviceID)
	if !h.runtime.knownDevice(input.DeviceID) {
		fail(w, 400, "unknown_device", "Выберите известный планшет")
		return
	}
	if !h.runtime.online(input.DeviceID, time.Now()) {
		fail(w, 409, "device_offline", "Планшет не подключён. Сначала откройте на нём экран показа")
		return
	}
	request := feedback.NewRatingRequestWithTTL(input.DeviceID, h.runtime.options.RequestTTL)
	if err := h.store.SaveRatingRequest(r.Context(), request); err != nil {
		storageError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, request)
}
func (h *Handler) GetRequest(w http.ResponseWriter, r *http.Request) {
	request, err := h.store.GetRatingRequest(r.Context(), r.PathValue("id"))
	if err != nil {
		storageError(w, err)
		return
	}
	writeJSON(w, 200, request)
}
func (h *Handler) TabletState(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("device_id")
	now := time.Now().UTC()
	h.runtime.touch(id, now)
	request, err := h.store.GetActiveRequest(r.Context(), id)
	if err != nil {
		storageError(w, err)
		return
	}
	url := ""
	if h.runtime.options.Slideshow != nil {
		url = "/slideshow/"
	}
	writeJSON(w, 200, map[string]any{"device_id": id, "server_now": now, "request": request, "slideshow_url": url, "slideshow_configured": url != ""})
}
func (h *Handler) TabletAck(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID string `json:"id"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if err := h.store.AcknowledgeRequest(r.Context(), input.ID, r.PathValue("device_id")); err != nil {
		storageError(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (h *Handler) TabletAnswer(w http.ResponseWriter, r *http.Request) {
	var input struct {
		RequestID string `json:"request_id"`
		Score     int    `json:"score"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	answer, err := h.store.AnswerRequest(r.Context(), input.RequestID, r.PathValue("device_id"), input.Score)
	if err != nil {
		storageError(w, err)
		return
	}
	writeJSON(w, 200, answer)
}
func (h *Handler) AdminState(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	devices := make([]map[string]any, 0, len(h.runtime.options.Devices))
	for _, device := range h.runtime.options.Devices {
		request, err := h.store.GetLatestRequest(r.Context(), device.ID)
		if err != nil {
			storageError(w, err)
			return
		}
		devices = append(devices, map[string]any{"id": device.ID, "name": device.Name, "online": h.runtime.online(device.ID, now), "last_seen": h.runtime.lastSeen(device.ID), "request": request})
	}
	writeJSON(w, 200, map[string]any{"devices": devices, "server_now": now, "request_ttl_seconds": int(h.runtime.options.RequestTTL.Seconds()), "slideshow_configured": h.runtime.options.Slideshow != nil})
}
