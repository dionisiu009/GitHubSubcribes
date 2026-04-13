package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// NewRouter збирає chi-роутер з усіма маршрутами та middleware
func NewRouter(h *Handler) http.Handler {
	r := chi.NewRouter()

	// --- Глобальні middleware ---
	r.Use(middleware.RequestID)                 // унікальний X-Request-Id для трейсингу
	r.Use(middleware.RealIP)                    // довіряємо X-Forwarded-For / X-Real-IP
	r.Use(middleware.Logger)                    // структурований лог кожного запиту
	r.Use(middleware.Recoverer)                 // перехоплює panic → 500
	r.Use(middleware.Timeout(30 * time.Second)) // дедлайн обробки запиту

	// --- Маршрути API ---
	r.Route("/api", func(r chi.Router) {
		// POST /api/subscribe
		r.Post("/subscribe", h.Subscribe)

		// GET /api/confirm/{token}
		r.Get("/confirm/{token}", h.ConfirmSubscription)

		// GET /api/unsubscribe/{token}
		r.Get("/unsubscribe/{token}", h.Unsubscribe)

		// GET /api/subscriptions?email={email}
		r.Get("/subscriptions", h.GetSubscriptions)
	})

	// --- Хелс-чек (зручно для Docker healthcheck та load balancer) ---
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	return r
}
