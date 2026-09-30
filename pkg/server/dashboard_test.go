package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/glorch/kestrel/pkg/store"
)

func TestDashboardRoutes(t *testing.T) {
	st := store.NewMemoryStore()
	srv := NewServer(st)
	ts := httptest.NewServer(srv.HTTPHandler())
	defer ts.Close()

	// 1. GET / should redirect to /dashboard
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // don't follow redirect
		},
	}
	respRoot, err := client.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("failed to request root: %v", err)
	}
	if respRoot.StatusCode != http.StatusFound {
		t.Errorf("expected 302 Found redirect, got: %d", respRoot.StatusCode)
	}
	loc := respRoot.Header.Get("Location")
	if loc != "/dashboard" {
		t.Errorf("expected redirect to /dashboard, got: %s", loc)
	}

	// 2. GET /dashboard should return 200 OK HTML
	respDashboard, err := http.Get(ts.URL + "/dashboard")
	if err != nil {
		t.Fatalf("failed to request /dashboard: %v", err)
	}
	if respDashboard.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got: %d", respDashboard.StatusCode)
	}
	contentType := respDashboard.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/html") {
		t.Errorf("expected text/html content-type, got: %s", contentType)
	}

	body, err := io.ReadAll(respDashboard.Body)
	_ = respDashboard.Body.Close()
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}

	bodyStr := string(body)
	if !strings.Contains(bodyStr, "Kestrel CI/CD Console") {
		t.Errorf("expected body to contain 'Kestrel CI/CD Console'")
	}
	if !strings.Contains(bodyStr, "Deployment Frequency") {
		t.Errorf("expected body to contain KPI indicators")
	}
}
