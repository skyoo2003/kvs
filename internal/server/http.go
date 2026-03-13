package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/skyoo2003/kvs"
)

type httpHandler struct {
	store *kvs.Store
}

type putRequest struct {
	Value string `json:"value"`
}

type getResponse struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func NewHTTPHandler(store *kvs.Store) http.Handler {
	if store == nil {
		store = kvs.NewStore()
	}

	handler := &httpHandler{store: store}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handler.handleHealth)
	mux.HandleFunc("/v1/keys/", handler.handleKey)

	return mux
}

func (h *httpHandler) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *httpHandler) handleKey(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/v1/keys/")
	if key == "" || key == r.URL.Path {
		writeJSONError(w, http.StatusBadRequest, "key is required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.handleGet(w, key)
	case http.MethodPut:
		h.handlePut(w, r, key)
	case http.MethodDelete:
		h.handleDelete(w, key)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (h *httpHandler) handleGet(w http.ResponseWriter, key string) {
	value, err := h.store.Get(key)
	if err != nil {
		if errors.Is(err, kvs.ErrKeyNotFound) {
			writeJSONError(w, http.StatusNotFound, err.Error())
			return
		}

		writeJSONError(w, http.StatusInternalServerError, "failed to read key")
		return
	}

	stringValue, ok := value.(string)
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "stored value is not a string")
		return
	}

	writeJSON(w, http.StatusOK, getResponse{Key: key, Value: stringValue})
}

func (h *httpHandler) handlePut(w http.ResponseWriter, r *http.Request, key string) {
	defer func() {
		_ = r.Body.Close()
	}()
	var req putRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.store.Put(key, req.Value); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to store key")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *httpHandler) handleDelete(w http.ResponseWriter, key string) {
	if err := h.store.Delete(key); err != nil {
		if errors.Is(err, kvs.ErrKeyNotFound) {
			writeJSONError(w, http.StatusNotFound, err.Error())
			return
		}

		writeJSONError(w, http.StatusInternalServerError, "failed to delete key")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, statusCode int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, statusCode int, message string) {
	writeJSON(w, statusCode, errorResponse{Error: message})
}
