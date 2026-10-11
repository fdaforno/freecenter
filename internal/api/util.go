package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"hostwarden/internal/esxi"
	"hostwarden/internal/store"
)

type ctxKey string

const userKey ctxKey = "user"

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func errJSON(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		errJSON(w, http.StatusBadRequest, "invalid json body: "+err.Error())
		return false
	}
	return true
}

// apiError maps internal errors to HTTP status codes.
func apiError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		errJSON(w, http.StatusNotFound, err.Error())
	case errors.Is(err, esxi.ErrNotFound):
		errJSON(w, http.StatusNotFound, err.Error())
	case errors.Is(err, store.ErrConflict):
		errJSON(w, http.StatusConflict, err.Error())
	case errors.Is(err, esxi.ErrInvalidState):
		errJSON(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrBadCreds):
		errJSON(w, http.StatusUnauthorized, "invalid credentials")
	case strings.Contains(err.Error(), "cannot shrink"),
		strings.Contains(err.Error(), "is not a disk"),
		strings.Contains(err.Error(), "is not a nic"),
		strings.Contains(err.Error(), "unknown power action"):
		errJSON(w, http.StatusBadRequest, err.Error())
	default:
		msg := err.Error()
		if strings.Contains(msg, "connect to") || strings.Contains(msg, "ssh dial") || strings.Contains(msg, "ssh handshake") {
			errJSON(w, http.StatusBadGateway, msg)
			return
		}
		// hostd task failures (faults) are client-fixable -> 400 with detail
		if strings.Contains(msg, "task") || strings.Contains(msg, "fault") {
			errJSON(w, http.StatusBadRequest, msg)
			return
		}
		errJSON(w, http.StatusInternalServerError, msg)
	}
}
