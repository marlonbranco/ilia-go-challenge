package middleware_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"ms-wallet/internal/domain"
	"ms-wallet/internal/middleware"
)

type mockStore struct {
	mu   sync.RWMutex
	data map[string]string
}

func newMockStore() *mockStore {
	return &mockStore{data: make(map[string]string)}
}

func (m *mockStore) SetNX(_ context.Context, key, value string, _ time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.data[key]; ok {
		return false, nil
	}
	m.data[key] = value
	return true, nil
}

func (m *mockStore) Set(_ context.Context, key, value string, _ time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = value
	return nil
}

func (m *mockStore) Get(_ context.Context, key string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if v, ok := m.data[key]; ok {
		return v, nil
	}
	return "", middleware.ErrKeyNotFound
}

func (m *mockStore) delete(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
}

func countingHandler(calls *int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"result":"created"}`))
	})
}

func wrap(store domain.IdempotencyStore, h http.Handler) http.Handler {
	return middleware.Idempotency(store)(h)
}

func TestIdempotency_FirstCall_ExecutesAndStores(t *testing.T) {
	store := newMockStore()
	calls := 0

	req := httptest.NewRequest(http.MethodPost, "/transactions", nil)
	req.Header.Set("Idempotency-Key", "key-first")
	w := httptest.NewRecorder()

	wrap(store, countingHandler(&calls)).ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("want 201, got %d", w.Code)
	}
	if calls != 1 {
		t.Errorf("want handler called once, got %d", calls)
	}
	if w.Body.String() == "" {
		t.Error("expected non-empty body")
	}
}

func TestIdempotency_DuplicateCall_ReturnsCachedWithout_Reexecuting(t *testing.T) {
	store := newMockStore()
	calls := 0
	h := wrap(store, countingHandler(&calls))
	key := "key-dup"

	req1 := httptest.NewRequest(http.MethodPost, "/transactions", nil)
	req1.Header.Set("Idempotency-Key", key)
	h.ServeHTTP(httptest.NewRecorder(), req1)

	req2 := httptest.NewRequest(http.MethodPost, "/transactions", nil)
	req2.Header.Set("Idempotency-Key", key)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("want 200 on replay, got %d", w2.Code)
	}
	if calls != 1 {
		t.Errorf("handler must not be called again; want 1, got %d", calls)
	}
	if w2.Body.String() != `{"result":"created"}` {
		t.Errorf("cached body mismatch: %s", w2.Body.String())
	}
}

func TestIdempotency_InFlight_Returns409(t *testing.T) {
	store := newMockStore()

	store.Set(context.Background(), "idempotency:inflight", "processing", 30*time.Second)

	calls := 0
	req := httptest.NewRequest(http.MethodPost, "/transactions", nil)
	req.Header.Set("Idempotency-Key", "inflight")
	w := httptest.NewRecorder()

	wrap(store, countingHandler(&calls)).ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("want 409, got %d", w.Code)
	}
	if calls != 0 {
		t.Errorf("handler must not be called; got %d", calls)
	}
}

func TestIdempotency_MissingHeader_Returns400(t *testing.T) {
	store := newMockStore()
	calls := 0
	req := httptest.NewRequest(http.MethodPost, "/transactions", nil)

	w := httptest.NewRecorder()

	wrap(store, countingHandler(&calls)).ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
	if calls != 0 {
		t.Errorf("handler must not be called; got %d", calls)
	}
}

func TestIdempotency_ExpiredKey_Reexecutes(t *testing.T) {
	store := newMockStore()
	calls := 0
	h := wrap(store, countingHandler(&calls))
	key := "key-expired"

	req1 := httptest.NewRequest(http.MethodPost, "/transactions", nil)
	req1.Header.Set("Idempotency-Key", key)
	h.ServeHTTP(httptest.NewRecorder(), req1)

	store.delete("idempotency:" + key)

	req2 := httptest.NewRequest(http.MethodPost, "/transactions", nil)
	req2.Header.Set("Idempotency-Key", key)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)

	if w2.Code != http.StatusCreated {
		t.Errorf("want 201 on re-execution, got %d", w2.Code)
	}
	if calls != 2 {
		t.Errorf("want handler called twice, got %d", calls)
	}
}

func BenchmarkIdempotency_FirstCall(b *testing.B) {
	store := newMockStore()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"result":"ok"}`))
	})
	h := middleware.Idempotency(store)(handler)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.Header.Set("Idempotency-Key", fmt.Sprintf("bench-%d", i))
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
}

func BenchmarkIdempotency_Replay(b *testing.B) {
	store := newMockStore()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"result":"ok"}`))
	})
	h := middleware.Idempotency(store)(handler)

	req0 := httptest.NewRequest(http.MethodPost, "/", nil)
	req0.Header.Set("Idempotency-Key", "bench-replay")
	h.ServeHTTP(httptest.NewRecorder(), req0)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.Header.Set("Idempotency-Key", "bench-replay")
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
}
