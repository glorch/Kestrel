package server

import (
	_ "embed"
	"net/http"
)

//go:embed dashboard.html
var DashboardHTML string

// RegisterDashboardRoutes registers the web dashboard on the HTTP mux.
func RegisterDashboardRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/dashboard", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(DashboardHTML))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/dashboard", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	})
}
