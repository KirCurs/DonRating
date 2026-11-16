package httpapi

import (
	"net/http"
	"time"
)

func NewRouter(store RatingScore) *http.ServeMux { return NewRouterWithOptions(store, Options{}) }
func NewRouterWithOptions(store RatingScore, options Options) *http.ServeMux {
	if len(options.Devices) == 0 {
		options.Devices = []Device{{ID: "reception-1", Name: "Ресепшен · планшет 1"}}
	}
	if options.RequestTTL == 0 {
		options.RequestTTL = 30 * time.Second
	}
	handler := Handler{store: store, runtime: &runtimeState{options: options, seen: make(map[string]time.Time)}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", Health)
	mux.HandleFunc("GET /version", Version)
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) {
		if options.Ready != nil {
			if err := options.Ready(r.Context()); err != nil {
				fail(w, 503, "not_ready", "База данных недоступна")
				return
			}
		}
		writeJSON(w, 200, map[string]bool{"ready": true})
	})
	mux.HandleFunc("POST /rating", handler.admin(handler.SubmitRating))
	mux.HandleFunc("GET /rating/{request_id}", handler.admin(handler.GetRating))
	mux.HandleFunc("POST /request", handler.admin(handler.CreateRatingRequest))
	mux.HandleFunc("POST /requests", handler.admin(handler.CreateRatingRequest))
	mux.HandleFunc("GET /requests/{id}", handler.admin(handler.GetRequest))
	mux.HandleFunc("GET /api/admin/state", handler.admin(handler.AdminState))
	mux.HandleFunc("GET /api/tablets/{device_id}/state", handler.tablet(handler.TabletState))
	mux.HandleFunc("POST /api/tablets/{device_id}/ack", handler.tablet(handler.TabletAck))
	mux.HandleFunc("POST /api/tablets/{device_id}/answer", handler.tablet(handler.TabletAnswer))
	static := frontendHandler()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/admin", http.StatusSeeOther) })
	for path, file := range map[string]string{"/admin": "/admin.html", "/tablet": "/tablet.html"} {
		mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) {
			clone := r.Clone(r.Context())
			clone.URL.Path = file
			static.ServeHTTP(w, clone)
		})
	}
	if options.Slideshow != nil {
		mux.Handle("GET /slideshow/", options.Slideshow)
	}
	mux.Handle("GET /", static)
	return mux
}
