package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIndexServesHTMLWithShowsFetch(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	Index().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Index() status = %d, want %d", rec.Code, http.StatusOK)
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.Contains(contentType, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", contentType)
	}
	if cacheControl := rec.Header().Get("Cache-Control"); cacheControl != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cacheControl)
	}
	if body := rec.Body.String(); !strings.Contains(body, "/api/shows") {
		t.Fatalf("Index() body does not contain /api/shows")
	}
}

func TestPlayerServesHTMLWithHLSAndChapters(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/player?show=demo", nil)

	Player().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Player() status = %d, want %d", rec.Code, http.StatusOK)
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.Contains(contentType, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", contentType)
	}
	if cacheControl := rec.Header().Get("Cache-Control"); cacheControl != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cacheControl)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "hls") {
		t.Fatalf("Player() body does not reference hls")
	}
	if !strings.Contains(body, "chapters.vtt") {
		t.Fatalf("Player() body does not reference chapters.vtt")
	}
}

func TestHandlerServesVendoredHLS(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/static/hls.min.js", nil)

	Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Handler() status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.Len() == 0 {
		t.Fatalf("Handler() served an empty hls.min.js body")
	}
}
