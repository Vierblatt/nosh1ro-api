package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestRateLimiter(t *testing.T) {
	rl := NewRateLimiter(10, 5)

	key := "test-ip"
	for i := range 5 {
		if !rl.Allow(key) {
			t.Errorf("request %d should be allowed", i+1)
		}
	}
	if rl.Allow(key) {
		t.Error("6th request should be denied after burst exhausted")
	}
}

func TestRateLimiter_Refill(t *testing.T) {
	rl := NewRateLimiter(100, 10)
	key := "test-ip"

	for range 10 {
		rl.Allow(key)
	}
	if rl.Allow(key) {
		t.Error("should be empty after burst exhausted")
	}

	rl.mu.Lock()
	b := rl.buckets[key]
	b.lastTime = time.Now().Add(-1 * time.Second)
	rl.mu.Unlock()

	if !rl.Allow(key) {
		t.Error("should allow after refill period")
	}
}

func TestRateLimiterMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RateLimit(100, 3))

	var hits int
	r.GET("/test", func(c *gin.Context) {
		hits++
		c.Status(http.StatusOK)
	})

	for i := range 3 {
		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("request %d: status = %d", i+1, w.Code)
		}
	}

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("rate limited status = %d, want %d", w.Code, http.StatusTooManyRequests)
	}
	if hits != 3 {
		t.Errorf("hits = %d, want 3", hits)
	}
}
