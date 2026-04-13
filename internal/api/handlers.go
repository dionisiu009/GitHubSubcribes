package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/dionisiu009/GitHubSubcribes/internal/models"
	"github.com/dionisiu009/GitHubSubcribes/internal/scanner"
	"github.com/dionisiu009/GitHubSubcribes/internal/service"
)

// Handler тримає залежності для всіх HTTP-обробників
type Handler struct {
	svc *service.SubscriptionService
}

// NewHandler створює Handler з сервісом
func NewHandler(svc *service.SubscriptionService) *Handler {
	return &Handler{svc: svc}
}

// =============================================================================
// POST /api/subscribe
// =============================================================================

// Subscribe — підписка email на релізи репозиторію
func (h *Handler) Subscribe(w http.ResponseWriter, r *http.Request) {
	var req models.SubscribeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "невалідне тіло запиту")
		return
	}

	err := h.svc.Subscribe(r.Context(), req.Email, req.Repo)
	if err == nil {
		writeJSON(w, http.StatusOK, map[string]string{
			"message": "підписка створена, перевірте email для підтвердження",
		})
		return
	}

	switch {
	case errors.Is(err, service.ErrInvalidEmail),
		errors.Is(err, service.ErrInvalidRepo):
		writeError(w, http.StatusBadRequest, err.Error())

	case errors.Is(err, service.ErrRepoNotFound):
		writeError(w, http.StatusNotFound, "репозиторій не знайдено на GitHub")

	case errors.Is(err, service.ErrAlreadySubscribed):
		writeError(w, http.StatusConflict, "email вже підписаний на цей репозиторій")

	case errors.Is(err, scanner.ErrRateLimit):
		writeError(w, http.StatusTooManyRequests, "перевищено ліміт GitHub API, спробуйте пізніше")

	default:
		writeError(w, http.StatusInternalServerError, "внутрішня помилка сервера")
	}
}

// =============================================================================
// GET /api/confirm/{token}
// =============================================================================

// ConfirmSubscription — підтвердження підписки за токеном
func (h *Handler) ConfirmSubscription(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")

	err := h.svc.Confirm(r.Context(), token)
	if err == nil {
		writeJSON(w, http.StatusOK, map[string]string{
			"message": "підписку підтверджено",
		})
		return
	}

	switch {
	case errors.Is(err, service.ErrInvalidRepo): // validateToken повертає ErrInvalidRepo
		writeError(w, http.StatusBadRequest, "невалідний токен")

	case errors.Is(err, service.ErrTokenNotFound):
		writeError(w, http.StatusNotFound, "токен не знайдено")

	default:
		writeError(w, http.StatusInternalServerError, "внутрішня помилка сервера")
	}
}

// =============================================================================
// GET /api/unsubscribe/{token}
// =============================================================================

// Unsubscribe — відписка за токеном
func (h *Handler) Unsubscribe(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")

	err := h.svc.Unsubscribe(r.Context(), token)
	if err == nil {
		writeJSON(w, http.StatusOK, map[string]string{
			"message": "відписку виконано успішно",
		})
		return
	}

	switch {
	case errors.Is(err, service.ErrInvalidRepo):
		writeError(w, http.StatusBadRequest, "невалідний токен")

	case errors.Is(err, service.ErrTokenNotFound):
		writeError(w, http.StatusNotFound, "токен не знайдено")

	default:
		writeError(w, http.StatusInternalServerError, "внутрішня помилка сервера")
	}
}

// =============================================================================
// GET /api/subscriptions?email={email}
// =============================================================================

// GetSubscriptions — список підписок для вказаного email
func (h *Handler) GetSubscriptions(w http.ResponseWriter, r *http.Request) {
	email := r.URL.Query().Get("email")

	subs, err := h.svc.GetSubscriptions(r.Context(), email)
	if err == nil {
		writeJSON(w, http.StatusOK, subs)
		return
	}

	switch {
	case errors.Is(err, service.ErrInvalidEmail):
		writeError(w, http.StatusBadRequest, "невалідний email")

	default:
		writeError(w, http.StatusInternalServerError, "внутрішня помилка сервера")
	}
}

// =============================================================================
// Утиліти відповіді
// =============================================================================

// errorResponse — уніфікована структура помилки у відповіді
type errorResponse struct {
	Error string `json:"error"`
}

// writeJSON серіалізує v як JSON з потрібним статусом
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError відправляє JSON з полем "error"
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}
