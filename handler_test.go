package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func setupTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	store, err := newStore(dir + "/blog.db")
	if err != nil {
		t.Fatalf("newStore: %v", err)
	}
	store.db.SetMaxOpenConns(10)
	if err := store.initSchema(context.Background()); err != nil {
		t.Fatalf("initSchema: %v", err)
	}
	t.Cleanup(func() { store.close() })
	return store
}

func seedTestPosts(t *testing.T, store *Store) {
	t.Helper()
	now := time.Now()
	posts := []Post{
		{ID: "post-1", Title: "First Post", Date: "2026-06-05", Status: "published", Category: "tech", Content: "hello", ContentHTML: "<p>hello</p>", Summary: "hello", CreatedAt: now, UpdatedAt: now},
		{ID: "post-2", Title: "Second Post", Date: "2026-06-04", Status: "published", Category: "life", Content: "world", ContentHTML: "<p>world</p>", Summary: "world", CreatedAt: now, UpdatedAt: now},
		{ID: "draft-1", Title: "Draft Post", Date: "2026-06-03", Status: "draft", Content: "secret", ContentHTML: "<p>secret</p>", Summary: "secret", CreatedAt: now, UpdatedAt: now},
	}
	for i := range posts {
		posts[i].Tags = []string{"go"}
		if err := store.insertPost(context.Background(), &posts[i]); err != nil {
			t.Fatalf("insertPost %s: %v", posts[i].ID, err)
		}
	}
}

func setupRouter(store *Store, cfg Config) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(corsMiddleware())

	api := r.Group("/api")
	{
		api.GET("/health", handleHealth)
		api.GET("/posts", handlePosts(store))
		api.GET("/posts/:id", handlePostDetail(store))
		api.GET("/tags", handleTags(store))
		api.GET("/feed.xml", handleFeed(store, cfg))
	}
	return r
}

func TestHandleHealth(t *testing.T) {
	r := setupRouter(setupTestStore(t), Config{})
	req := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	var body map[string]string
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["status"] != "ok" {
		t.Errorf("status = %q, want %q", body["status"], "ok")
	}
}

func TestHandlePosts(t *testing.T) {
	store := setupTestStore(t)
	seedTestPosts(t, store)
	r := setupRouter(store, Config{})

	req := httptest.NewRequest("GET", "/api/posts?page=1&size=10", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}

	var result PostListResult
	json.Unmarshal(w.Body.Bytes(), &result)
	if len(result.Posts) != 2 { // drafts excluded
		t.Errorf("got %d posts, want 2", len(result.Posts))
	}
	if result.Total != 2 {
		t.Errorf("total = %d, want 2", result.Total)
	}
	if result.Page != 1 {
		t.Errorf("page = %d, want 1", result.Page)
	}
	// Posts should be ordered by date desc
	if result.Posts[0].ID != "post-1" {
		t.Errorf("first post = %q, want post-1", result.Posts[0].ID)
	}
}

func TestHandlePosts_FilterByCategory(t *testing.T) {
	store := setupTestStore(t)
	seedTestPosts(t, store)
	r := setupRouter(store, Config{})

	req := httptest.NewRequest("GET", "/api/posts?category=tech", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var result PostListResult
	json.Unmarshal(w.Body.Bytes(), &result)
	if len(result.Posts) != 1 {
		t.Errorf("got %d posts, want 1", len(result.Posts))
	}
}

func TestHandlePosts_FilterByTag(t *testing.T) {
	store := setupTestStore(t)
	seedTestPosts(t, store)
	r := setupRouter(store, Config{})

	req := httptest.NewRequest("GET", "/api/posts?tag=go", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var result PostListResult
	json.Unmarshal(w.Body.Bytes(), &result)
	if len(result.Posts) != 2 {
		t.Errorf("got %d posts, want 2", len(result.Posts))
	}
}

func TestHandlePosts_Search(t *testing.T) {
	store := setupTestStore(t)
	seedTestPosts(t, store)
	r := setupRouter(store, Config{})

	req := httptest.NewRequest("GET", "/api/posts?q=hello", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var result PostListResult
	json.Unmarshal(w.Body.Bytes(), &result)
	if len(result.Posts) != 1 || result.Posts[0].ID != "post-1" {
		t.Errorf("search should find post-1, got %d results", len(result.Posts))
	}
}

func TestHandlePostDetail(t *testing.T) {
	store := setupTestStore(t)
	seedTestPosts(t, store)
	r := setupRouter(store, Config{})

	req := httptest.NewRequest("GET", "/api/posts/post-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var body map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["id"] != "post-1" {
		t.Errorf("id = %v", body["id"])
	}
	if body["content_html"] != "<p>hello</p>" {
		t.Errorf("unexpected content_html")
	}
}

func TestHandlePostDetail_NotFound(t *testing.T) {
	store := setupTestStore(t)
	r := setupRouter(store, Config{})

	req := httptest.NewRequest("GET", "/api/posts/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandlePostDetail_DraftHidden(t *testing.T) {
	store := setupTestStore(t)
	seedTestPosts(t, store)
	r := setupRouter(store, Config{})

	req := httptest.NewRequest("GET", "/api/posts/draft-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("draft should return 404, got %d", w.Code)
	}
}

func TestHandleTags(t *testing.T) {
	store := setupTestStore(t)
	seedTestPosts(t, store)
	r := setupRouter(store, Config{})

	req := httptest.NewRequest("GET", "/api/tags", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var body map[string][]string
	json.Unmarshal(w.Body.Bytes(), &body)
	if len(body["tags"]) == 0 {
		t.Error("expected non-empty tags")
	}
}

func TestHandleFeed(t *testing.T) {
	store := setupTestStore(t)
	seedTestPosts(t, store)
	r := setupRouter(store, Config{BlogTitle: "test-blog"})

	req := httptest.NewRequest("GET", "/api/feed.xml", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if ct != "application/rss+xml; charset=utf-8" {
		t.Errorf("content-type = %q", ct)
	}
	body := w.Body.String()
	if body == "" || body[:5] != `<?xml` {
		t.Errorf("not valid XML feed: %s", body[:100])
	}
}

func TestHandleAdminLogin(t *testing.T) {
	store := setupTestStore(t)
	hash, _ := hashPassword("admin123")
	store.upsertAdmin(context.Background(), "admin", hash)
	r := setupAdminRouter(store, Config{JWTSecret: "jwt-secret", AdminUsername: "admin"})

	body := `{"username":"admin","password":"admin123"}`
	req := httptest.NewRequest("POST", "/api/admin/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["token"] == "" {
		t.Error("expected non-empty token")
	}
}

func TestHandleAdminLogin_WrongPassword(t *testing.T) {
	store := setupTestStore(t)
	hash, _ := hashPassword("admin123")
	store.upsertAdmin(context.Background(), "admin", hash)
	r := setupAdminRouter(store, Config{JWTSecret: "jwt", AdminUsername: "admin"})

	body := `{"username":"admin","password":"wrong"}`
	req := httptest.NewRequest("POST", "/api/admin/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleAdminCRUD(t *testing.T) {
	store := setupTestStore(t)
	hash, _ := hashPassword("admin123")
	store.upsertAdmin(context.Background(), "admin", hash)
	cfg := Config{JWTSecret: "jwt-secret", AdminUsername: "admin"}
	r := setupAdminRouter(store, cfg)

	// Login
	loginBody := `{"username":"admin","password":"admin123"}`
	req := httptest.NewRequest("POST", "/api/admin/login", strings.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var loginResp map[string]string
	json.Unmarshal(w.Body.Bytes(), &loginResp)
	token := loginResp["token"]

	authHeader := func() string { return "Bearer " + token }

	// Create
	t.Run("create", func(t *testing.T) {
		body := `{"id":"new-post","title":"New Post","content":"hello","status":"published","tags":["go","api"]}`
		req := httptest.NewRequest("POST", "/api/admin/posts", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", authHeader())
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("create status = %d, body = %s", w.Code, w.Body.String())
		}
	})

	// Read
	t.Run("list", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/admin/posts", nil)
		req.Header.Set("Authorization", authHeader())
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("list status = %d", w.Code)
		}
		var result PostListResult
		json.Unmarshal(w.Body.Bytes(), &result)
		if result.Total != 1 {
			t.Errorf("total = %d, want 1", result.Total)
		}
	})

	// Update
	t.Run("update", func(t *testing.T) {
		body := `{"title":"Updated Title"}`
		req := httptest.NewRequest("PUT", "/api/admin/posts/new-post", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", authHeader())
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("update status = %d, body = %s", w.Code, w.Body.String())
		}
	})

	// Delete
	t.Run("delete", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/api/admin/posts/new-post", nil)
		req.Header.Set("Authorization", authHeader())
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("delete status = %d", w.Code)
		}
	})
}

func setupAdminRouter(store *Store, cfg Config) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	admin := r.Group("/api/admin")
	admin.POST("/login", handleAdminLogin(store, cfg))

	protected := admin.Group("")
	protected.Use(authMiddleware(cfg.JWTSecret))
	{
		protected.GET("/posts", handleAdminListPosts(store))
		protected.POST("/posts", handleAdminCreatePost(store))
		protected.PUT("/posts/:id", handleAdminUpdatePost(store))
		protected.DELETE("/posts/:id", handleAdminDeletePost(store))
	}
	return r
}

