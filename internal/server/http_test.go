package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/skyoo2003/kvs"
)

func newTestRequest(t *testing.T, method, target string, body io.Reader) *http.Request {
	t.Helper()

	return httptest.NewRequestWithContext(t.Context(), method, target, body)
}

func TestHTTPHandlerHealth(t *testing.T) {
	handler := NewHTTPHandler(kvs.NewStore())
	req := newTestRequest(t, http.MethodGet, "/healthz", http.NoBody)
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusOK)
	}
}

func TestHTTPHandlerPutGetDelete(t *testing.T) {
	const (
		key   = "language"
		value = "go"
	)

	handler := NewHTTPHandler(kvs.NewStore())

	putReq := newTestRequest(t, http.MethodPut, "/v1/keys/"+key, strings.NewReader(`{"value":"go"}`))
	putRes := httptest.NewRecorder()
	handler.ServeHTTP(putRes, putReq)
	if putRes.Code != http.StatusNoContent {
		t.Fatalf("put status = %d, want %d", putRes.Code, http.StatusNoContent)
	}

	getReq := newTestRequest(t, http.MethodGet, "/v1/keys/"+key, http.NoBody)
	getRes := httptest.NewRecorder()
	handler.ServeHTTP(getRes, getReq)
	if getRes.Code != http.StatusOK {
		t.Fatalf("get status = %d, want %d", getRes.Code, http.StatusOK)
	}

	var body getResponse
	if err := json.Unmarshal(getRes.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if body.Key != key || body.Value != value {
		t.Fatalf("get response = %+v, want key/value", body)
	}

	deleteReq := newTestRequest(t, http.MethodDelete, "/v1/keys/"+key, http.NoBody)
	deleteRes := httptest.NewRecorder()
	handler.ServeHTTP(deleteRes, deleteReq)
	if deleteRes.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want %d", deleteRes.Code, http.StatusNoContent)
	}
}

// TestHTTPHandlerRejectsUnsupportedMethod covers the promise the HTTP API page makes about every
// error carrying a JSON body, which a bare WriteHeader would break for 405 alone.
func TestHTTPHandlerRejectsUnsupportedMethod(t *testing.T) {
	for _, target := range []string{"/healthz", "/v1/keys/language"} {
		handler := NewHTTPHandler(kvs.NewStore())
		req := newTestRequest(t, http.MethodPost, target, http.NoBody)
		res := httptest.NewRecorder()

		handler.ServeHTTP(res, req)

		if res.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST %s status = %d, want %d", target, res.Code, http.StatusMethodNotAllowed)
		}
		if got := res.Header().Get("Content-Type"); got != "application/json" {
			t.Errorf("POST %s content type = %q, want application/json", target, got)
		}

		var body errorResponse
		if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
			t.Errorf("POST %s json.Unmarshal(%q) error = %v", target, res.Body.String(), err)
			continue
		}
		if body.Error == "" {
			t.Errorf("POST %s error message is empty", target)
		}
	}
}

func TestHTTPHandlerMissingKey(t *testing.T) {
	handler := NewHTTPHandler(kvs.NewStore())
	req := newTestRequest(t, http.MethodGet, "/v1/keys/missing", http.NoBody)
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusNotFound)
	}
}

func TestHTTPHandlerDeleteMissingKey(t *testing.T) {
	handler := NewHTTPHandler(kvs.NewStore())
	req := newTestRequest(t, http.MethodDelete, "/v1/keys/missing", http.NoBody)
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusNotFound)
	}
}

func TestHTTPHandlerPutOverwritesValue(t *testing.T) {
	handler := NewHTTPHandler(kvs.NewStore())

	firstReq := newTestRequest(t, http.MethodPut, "/v1/keys/language", strings.NewReader(`{"value":"go"}`))
	firstRes := httptest.NewRecorder()
	handler.ServeHTTP(firstRes, firstReq)

	secondReq := newTestRequest(t, http.MethodPut, "/v1/keys/language", strings.NewReader(`{"value":"rust"}`))
	secondRes := httptest.NewRecorder()
	handler.ServeHTTP(secondRes, secondReq)

	getReq := newTestRequest(t, http.MethodGet, "/v1/keys/language", http.NoBody)
	getRes := httptest.NewRecorder()
	handler.ServeHTTP(getRes, getReq)

	var body getResponse
	if err := json.Unmarshal(getRes.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if body.Value != "rust" {
		t.Fatalf("value = %q, want %q", body.Value, "rust")
	}
}

func TestHTTPHandlerSupportsSlashInKey(t *testing.T) {
	handler := NewHTTPHandler(kvs.NewStore())

	putReq := newTestRequest(t, http.MethodPut, "/v1/keys/team/backend", strings.NewReader(`{"value":"go"}`))
	putRes := httptest.NewRecorder()
	handler.ServeHTTP(putRes, putReq)

	getReq := newTestRequest(t, http.MethodGet, "/v1/keys/team/backend", http.NoBody)
	getRes := httptest.NewRecorder()
	handler.ServeHTTP(getRes, getReq)

	var body getResponse
	if err := json.Unmarshal(getRes.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if body.Key != "team/backend" {
		t.Fatalf("key = %q, want %q", body.Key, "team/backend")
	}
}

func TestHTTPHandlerRejectsInvalidBody(t *testing.T) {
	handler := NewHTTPHandler(kvs.NewStore())
	req := newTestRequest(t, http.MethodPut, "/v1/keys/language", strings.NewReader(`{"value":"go"`))
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusBadRequest)
	}
}
