package middleware

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"ms-wallet/internal/domain"
)

const (
	processingVal = "processing"
	keyPrefix     = "idempotency:"
	lockTTL       = 30 * time.Second
	resultTTL     = 24 * time.Hour
)

var ErrKeyNotFound = errors.New("idempotency key not found")

type cachedEntry struct {
	Status int    `json:"s"`
	Body   []byte `json:"b"`
}

type bufferedWriter struct {
	header http.Header
	status int
	buf    bytes.Buffer
	wrote  bool
}

func newBufferedWriter() *bufferedWriter {
	return &bufferedWriter{header: make(http.Header), status: http.StatusOK}
}

func (bw *bufferedWriter) Header() http.Header { return bw.header }

func (bw *bufferedWriter) WriteHeader(code int) {
	if !bw.wrote {
		bw.status = code
		bw.wrote = true
	}
}

func (bw *bufferedWriter) Write(b []byte) (int, error) {
	if !bw.wrote {
		bw.WriteHeader(http.StatusOK)
	}
	return bw.buf.Write(b)
}

func (bw *bufferedWriter) flush(w http.ResponseWriter) {
	for k, vs := range bw.header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(bw.status)
	w.Write(bw.buf.Bytes())
}

func Idempotency(store domain.IdempotencyStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			idemKey := r.Header.Get("Idempotency-Key")
			if idemKey == "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"error":"Idempotency-Key header is required"}`))
				return
			}

			redisKey := keyPrefix + idemKey

			acquired, err := store.SetNX(r.Context(), redisKey, processingVal, lockTTL)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"error":"idempotency store unavailable"}`))
				return
			}

			if !acquired {
				val, err := store.Get(r.Context(), redisKey)
				if err != nil {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusConflict)
					w.Write([]byte(`{"error":"request in progress, retry after backoff"}`))
					return
				}

				if val == processingVal {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusConflict)
					w.Write([]byte(`{"error":"request in progress, retry after backoff"}`))
					return
				}

				var entry cachedEntry
				if err := json.Unmarshal([]byte(val), &entry); err != nil {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					w.Write([]byte(`{"error":"failed to decode cached response"}`))
					return
				}

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write(entry.Body)
				return
			}

			buf := newBufferedWriter()
			next.ServeHTTP(buf, r)

			entry, _ := json.Marshal(cachedEntry{Status: buf.status, Body: buf.buf.Bytes()})
			_ = store.Set(r.Context(), redisKey, string(entry), resultTTL)

			buf.flush(w)
		})
	}
}
