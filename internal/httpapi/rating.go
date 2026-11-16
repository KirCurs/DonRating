package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"example.com/ratingdon/internal/feedback"
)

type RatingScore interface {
	GetRating(context.Context, string) (feedback.Rating, error)
	SaveRatingRequest(context.Context, feedback.RatingRequest) error
	GetRatingRequest(context.Context, string) (feedback.RatingRequest, error)
	GetActiveRequest(context.Context, string) (*feedback.RatingRequest, error)
	GetLatestRequest(context.Context, string) (*feedback.RatingRequest, error)
	AcknowledgeRequest(context.Context, string, string) error
	AnswerRequest(context.Context, string, string, int) (feedback.RatingRequest, error)
}

type Handler struct {
	store   RatingScore
	runtime *runtimeState
}
type apiError struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Warn("Не удалось записать HTTP-ответ", "error", err)
	}
}
func fail(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, apiError{code, message})
}
func decodeJSON(w http.ResponseWriter, r *http.Request, value any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		fail(w, 400, "invalid_json", "Некорректный JSON")
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		fail(w, 400, "invalid_json", "Ожидается один JSON-объект")
		return false
	}
	return true
}
func storageError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, feedback.ErrRequestNotFound), errors.Is(err, feedback.ErrRatingNotFound):
		fail(w, 404, "not_found", "Запрос оценки не найден")
	case errors.Is(err, feedback.ErrRequestExpired):
		fail(w, 410, "expired", "Время для ответа истекло")
	case errors.Is(err, feedback.ErrRequestActive):
		fail(w, 409, "active_request", "На планшете уже открыт запрос оценки")
	case errors.Is(err, feedback.ErrRatingAlreadyExists), errors.Is(err, feedback.ErrRequestClosed):
		fail(w, 409, "already_answered", "Оценка уже принята")
	case errors.Is(err, feedback.ErrInvalidRequest):
		fail(w, 400, "invalid_request", "Проверьте данные запроса и оценку от 1 до 5")
	default:
		slog.Error("Ошибка хранилища", "error", err)
		fail(w, 500, "storage_error", "Не удалось сохранить или прочитать данные. Попробуйте ещё раз")
	}
}

// SubmitRating retains the administrator API, with the same request lifecycle
// as tablet answers. Unissued identifiers cannot create orphan ratings.
func (h *Handler) SubmitRating(w http.ResponseWriter, r *http.Request) {
	var rating feedback.Rating
	if !decodeJSON(w, r, &rating) {
		return
	}
	rating.RequestID = strings.TrimSpace(rating.RequestID)
	if rating.Validate() != nil {
		fail(w, 400, "invalid_rating", "Укажите запрос и оценку от 1 до 5")
		return
	}
	request, err := h.store.GetRatingRequest(r.Context(), rating.RequestID)
	if err != nil {
		storageError(w, err)
		return
	}
	answer, err := h.store.AnswerRequest(r.Context(), request.ID, request.DeviceID, rating.Score)
	if err != nil {
		storageError(w, err)
		return
	}
	writeJSON(w, 200, answer)
}
func (h *Handler) GetRating(w http.ResponseWriter, r *http.Request) {
	rating, err := h.store.GetRating(r.Context(), r.PathValue("request_id"))
	if err != nil {
		storageError(w, err)
		return
	}
	writeJSON(w, 200, rating)
}
