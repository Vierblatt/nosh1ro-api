package handler

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Vierblatt/nosh1ro-api/internal/auth"
	"github.com/Vierblatt/nosh1ro-api/internal/model"
	"github.com/Vierblatt/nosh1ro-api/internal/store"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/pbkdf2"
)

func setupTestDB(t *testing.T) *store.Store {
	t.Helper()
	dsn := os.Getenv("TEST_DSN")
	if dsn == "" {
		dsn = "root:password@tcp(127.0.0.1:3306)/blog_test?charset=utf8mb4&parseTime=true&loc=Local&multiStatements=true"
	}
	s, err := store.New(dsn)
	if err != nil {
		t.Skipf("skipping test — cannot connect to MySQL (set TEST_DSN): %v", err)
	}
	if err := s.Ping(context.Background()); err != nil {
		t.Skipf("skipping test — MySQL not reachable (set TEST_DSN): %v", err)
	}
	if err := s.InitSchema(context.Background()); err != nil {
		t.Fatalf("InitSchema: %v", err)
	}
	// 清空测试数据，保证每个测试都从干净状态开始
	s.ResetAll(context.Background())
	t.Cleanup(func() { s.Close() })
	return s
}

func seedTestPosts(t *testing.T, s *store.Store) {
	t.Helper()
	now := time.Now()
	posts := []model.Post{
		{ID: "post-1", Title: "First Post", Date: "2026-06-05", Status: "published", Category: "tech", Content: "hello", ContentHTML: "<p>hello</p>", Summary: "hello", CreatedAt: now, UpdatedAt: now},
		{ID: "post-2", Title: "Second Post", Date: "2026-06-04", Status: "published", Category: "life", Content: "world", ContentHTML: "<p>world</p>", Summary: "world", CreatedAt: now, UpdatedAt: now},
		{ID: "draft-1", Title: "Draft Post", Date: "2026-06-03", Status: "draft", Content: "secret", ContentHTML: "<p>secret</p>", Summary: "secret", CreatedAt: now, UpdatedAt: now},
	}
	for i := range posts {
		posts[i].Tags = []string{"go"}
		if err := s.InsertPost(context.Background(), &posts[i]); err != nil {
			t.Fatalf("InsertPost %s: %v", posts[i].ID, err)
		}
	}
}

func newTestRouter(s *store.Store) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())

	pc := NewPostController(s, nil, nil)
	pc.Register(r.Group("/api"))

	fc := NewFeedController(s, s, "test-blog")
	r.GET("/api/feed.xml", fc.Handle)

	return r
}

func newTestAdminRouter(s *store.Store, jwtSecret string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())

	ac := NewAdminController(s, s, jwtSecret, nil, nil, nil)
	ac.Register(r.Group("/api/admin"))

	return r
}

func TestHandleHealth(t *testing.T) {
	r := newTestRouter(setupTestDB(t))
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
	s := setupTestDB(t)
	seedTestPosts(t, s)
	r := newTestRouter(s)

	req := httptest.NewRequest("GET", "/api/posts?page=1&size=10", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}

	var result model.PostListResult
	json.Unmarshal(w.Body.Bytes(), &result)
	if len(result.Posts) != 2 {
		t.Errorf("got %d posts, want 2", len(result.Posts))
	}
	if result.Total != 2 {
		t.Errorf("total = %d, want 2", result.Total)
	}
	if result.Page != 1 {
		t.Errorf("page = %d, want 1", result.Page)
	}
	if result.Posts[0].ID != "post-1" {
		t.Errorf("first post = %q, want post-1", result.Posts[0].ID)
	}
}

func TestHandlePosts_FilterByCategory(t *testing.T) {
	s := setupTestDB(t)
	seedTestPosts(t, s)
	r := newTestRouter(s)

	req := httptest.NewRequest("GET", "/api/posts?category=tech", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var result model.PostListResult
	json.Unmarshal(w.Body.Bytes(), &result)
	if len(result.Posts) != 1 {
		t.Errorf("got %d posts, want 1", len(result.Posts))
	}
}

func TestHandlePosts_FilterByTag(t *testing.T) {
	s := setupTestDB(t)
	seedTestPosts(t, s)
	r := newTestRouter(s)

	req := httptest.NewRequest("GET", "/api/posts?tag=go", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var result model.PostListResult
	json.Unmarshal(w.Body.Bytes(), &result)
	if len(result.Posts) != 2 {
		t.Errorf("got %d posts, want 2", len(result.Posts))
	}
}

func TestHandlePosts_Search(t *testing.T) {
	s := setupTestDB(t)
	seedTestPosts(t, s)
	r := newTestRouter(s)

	req := httptest.NewRequest("GET", "/api/posts?q=hello", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var result model.PostListResult
	json.Unmarshal(w.Body.Bytes(), &result)
	if len(result.Posts) != 1 || result.Posts[0].ID != "post-1" {
		t.Errorf("search should find post-1, got %d results", len(result.Posts))
	}
}

func TestHandlePostDetail(t *testing.T) {
	s := setupTestDB(t)
	seedTestPosts(t, s)
	r := newTestRouter(s)

	req := httptest.NewRequest("GET", "/api/posts/post-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["id"] != "post-1" {
		t.Errorf("id = %v", body["id"])
	}
	if body["content_html"] != "<p>hello</p>" {
		t.Errorf("unexpected content_html")
	}
}

func TestHandlePostDetail_NotFound(t *testing.T) {
	s := setupTestDB(t)
	r := newTestRouter(s)

	req := httptest.NewRequest("GET", "/api/posts/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandlePostDetail_DraftHidden(t *testing.T) {
	s := setupTestDB(t)
	seedTestPosts(t, s)
	r := newTestRouter(s)

	req := httptest.NewRequest("GET", "/api/posts/draft-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("draft should return 404, got %d", w.Code)
	}
}

func TestHandleTags(t *testing.T) {
	s := setupTestDB(t)
	seedTestPosts(t, s)
	r := newTestRouter(s)

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
	s := setupTestDB(t)
	seedTestPosts(t, s)
	r := newTestRouter(s)

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
	s := setupTestDB(t)
	hash, _ := auth.HashPassword("admin123")
	s.UpsertAdmin(context.Background(), "admin", hash)
	r := newTestAdminRouter(s, "jwt-secret")

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
	s := setupTestDB(t)
	hash, _ := auth.HashPassword("admin123")
	s.UpsertAdmin(context.Background(), "admin", hash)
	r := newTestAdminRouter(s, "jwt")

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
	s := setupTestDB(t)
	hash, _ := auth.HashPassword("admin123")
	s.UpsertAdmin(context.Background(), "admin", hash)
	r := newTestAdminRouter(s, "jwt-secret")

	loginBody := `{"username":"admin","password":"admin123"}`
	req := httptest.NewRequest("POST", "/api/admin/login", strings.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var loginResp map[string]string
	json.Unmarshal(w.Body.Bytes(), &loginResp)
	token := loginResp["token"]

	authHeader := func() string { return "Bearer " + token }

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

	t.Run("list", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/admin/posts", nil)
		req.Header.Set("Authorization", authHeader())
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("list status = %d", w.Code)
		}
		var result model.PostListResult
		json.Unmarshal(w.Body.Bytes(), &result)
		if result.Total != 1 {
			t.Errorf("total = %d, want 1", result.Total)
		}
	})

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

func TestHandleVerify(t *testing.T) {
	s := setupTestDB(t)
	seedTestPosts(t, s)

	salt := make([]byte, 16)
	nonce := make([]byte, 12)
	io.ReadFull(rand.Reader, salt)
	io.ReadFull(rand.Reader, nonce)
	password := "test-password"
	key := pbkdf2.Key([]byte(password), salt, 100000, 32, sha256.New)
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	ciphertext := gcm.Seal(nil, nonce, []byte("secret content"), nil)

	encPost := &model.Post{
		ID: "enc-1", Title: "Enc Post", Date: "2026-06-01", Status: "published",
		Content: "", ContentHTML: "", Summary: "encrypted",
		Encrypted: true,
		Encryption: &model.EncryptionData{
			Salt:       base64.StdEncoding.EncodeToString(salt),
			Nonce:      base64.StdEncoding.EncodeToString(nonce),
			Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
		},
		Tags:      []string{"secret"},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := s.InsertPost(context.Background(), encPost); err != nil {
		t.Fatalf("insert encrypted post: %v", err)
	}

	r := newTestRouter(s)

	t.Run("correct password", func(t *testing.T) {
		body := `{"password":"test-password"}`
		req := httptest.NewRequest("POST", "/api/posts/enc-1/verify", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
		}
		var resp map[string]string
		json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["content"] != "secret content" {
			t.Errorf("content = %q, want %q", resp["content"], "secret content")
		}
	})

	t.Run("wrong password", func(t *testing.T) {
		body := `{"password":"wrong"}`
		req := httptest.NewRequest("POST", "/api/posts/enc-1/verify", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
		}
	})

	t.Run("not encrypted", func(t *testing.T) {
		body := `{"password":"pw"}`
		req := httptest.NewRequest("POST", "/api/posts/post-1/verify", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
		}
	})

	t.Run("not found", func(t *testing.T) {
		body := `{"password":"pw"}`
		req := httptest.NewRequest("POST", "/api/posts/nonexistent/verify", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
		}
	})

	t.Run("empty password", func(t *testing.T) {
		body := `{}`
		req := httptest.NewRequest("POST", "/api/posts/enc-1/verify", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
		}
	})
}
