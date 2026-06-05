package main

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type PostFilter struct {
	Status   string
	Tag      string
	Category string
	Search   string
}

type PostListResult struct {
	Posts []Post `json:"posts"`
	Total int64  `json:"total"`
	Page  int64  `json:"page"`
	Size  int64  `json:"size"`
}

type Store struct {
	db *sql.DB
}

func newStore(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("sqlite open: %w", err)
	}
	db.SetMaxOpenConns(1)
	return &Store{db: db}, nil
}

func (s *Store) initSchema(ctx context.Context) error {
	schema := `
	CREATE TABLE IF NOT EXISTS posts (
		id          TEXT PRIMARY KEY,
		title       TEXT NOT NULL,
		content     TEXT NOT NULL DEFAULT '',
		content_html TEXT NOT NULL DEFAULT '',
		summary     TEXT NOT NULL DEFAULT '',
		date        TEXT NOT NULL,
		tags        TEXT NOT NULL DEFAULT '[]',
		category    TEXT NOT NULL DEFAULT '',
		status      TEXT NOT NULL DEFAULT 'draft',
		encrypted   INTEGER NOT NULL DEFAULT 0,
		enc_salt     TEXT NOT NULL DEFAULT '',
		enc_nonce    TEXT NOT NULL DEFAULT '',
		enc_cipher   TEXT NOT NULL DEFAULT '',
		created_at  TEXT NOT NULL,
		updated_at  TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_posts_date ON posts(date DESC);
	CREATE INDEX IF NOT EXISTS idx_posts_status ON posts(status);

	CREATE TABLE IF NOT EXISTS admin (
		username      TEXT PRIMARY KEY,
		password_hash TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS settings (
		id       INTEGER PRIMARY KEY DEFAULT 1,
		title    TEXT NOT NULL DEFAULT '',
		subtitle TEXT NOT NULL DEFAULT ''
	);
	INSERT OR IGNORE INTO settings (id, title, subtitle) VALUES (1, '', '');
	`
	_, err := s.db.ExecContext(ctx, schema)
	return err
}

func (s *Store) close() error { return s.db.Close() }

// Post CRUD

func (s *Store) findPosts(ctx context.Context, f PostFilter, page, size int64) (*PostListResult, error) {
	var conditions []string
	var args []interface{}

	if f.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, f.Status)
	}
	if f.Tag != "" {
		conditions = append(conditions, "tags LIKE ?")
		args = append(args, `%"`+f.Tag+`"%`)
	}
	if f.Category != "" {
		conditions = append(conditions, "category = ?")
		args = append(args, f.Category)
	}
	if f.Search != "" {
		conditions = append(conditions, "(title LIKE ? OR content LIKE ?)")
		q := "%" + f.Search + "%"
		args = append(args, q, q)
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	var total int64
	countSQL := "SELECT COUNT(*) FROM posts " + where
	if err := s.db.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, err
	}

	offset := (page - 1) * size
	selectSQL := "SELECT id, title, content, content_html, summary, date, tags, category, status, encrypted, enc_salt, enc_nonce, enc_cipher, created_at, updated_at FROM posts " + where + " ORDER BY date DESC LIMIT ? OFFSET ?"
	args = append(args, size, offset)

	rows, err := s.db.QueryContext(ctx, selectSQL, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	posts, err := scanPosts(rows)
	if err != nil {
		return nil, err
	}
	return &PostListResult{Posts: posts, Total: total, Page: page, Size: size}, nil
}

func (s *Store) countPosts(ctx context.Context, f PostFilter) (int64, error) {
	var conditions []string
	var args []interface{}
	if f.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, f.Status)
	}
	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}
	var count int64
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM posts "+where, args...).Scan(&count)
	return count, err
}

func (s *Store) findPost(ctx context.Context, id string) (*Post, error) {
	row := s.db.QueryRowContext(ctx,
		"SELECT id, title, content, content_html, summary, date, tags, category, status, encrypted, enc_salt, enc_nonce, enc_cipher, created_at, updated_at FROM posts WHERE id = ?", id)
	return scanPost(row)
}

func (s *Store) insertPost(ctx context.Context, p *Post) error {
	enc := encryptionFromPost(p)
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO posts (id, title, content, content_html, summary, date, tags, category, status, encrypted, enc_salt, enc_nonce, enc_cipher, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
		p.ID, p.Title, p.Content, p.ContentHTML, p.Summary, p.Date, joinTags(p.Tags), p.Category, p.Status, boolToInt(p.Encrypted),
		enc.Salt, enc.Nonce, enc.Ciphertext, p.CreatedAt.Format(time.RFC3339), p.UpdatedAt.Format(time.RFC3339))
	return err
}

func (s *Store) replacePost(ctx context.Context, p *Post) error {
	enc := encryptionFromPost(p)
	_, err := s.db.ExecContext(ctx,
		"UPDATE posts SET title=?, content=?, content_html=?, summary=?, date=?, tags=?, category=?, status=?, encrypted=?, enc_salt=?, enc_nonce=?, enc_cipher=?, updated_at=? WHERE id=?",
		p.Title, p.Content, p.ContentHTML, p.Summary, p.Date, joinTags(p.Tags), p.Category, p.Status, boolToInt(p.Encrypted),
		enc.Salt, enc.Nonce, enc.Ciphertext, p.UpdatedAt.Format(time.RFC3339), p.ID)
	return err
}

func (s *Store) deletePost(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM posts WHERE id = ?", id)
	return err
}

func (s *Store) postExists(ctx context.Context, id string) bool {
	var exists int
	s.db.QueryRowContext(ctx, "SELECT 1 FROM posts WHERE id = ?", id).Scan(&exists)
	return exists == 1
}

func (s *Store) distinctTags(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT DISTINCT tags FROM posts WHERE status = 'published'")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tagSet := make(map[string]struct{})
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			continue
		}
		for _, t := range splitTags(raw) {
			if t != "" {
				tagSet[t] = struct{}{}
			}
		}
	}
	tags := make([]string, 0, len(tagSet))
	for t := range tagSet {
		tags = append(tags, t)
	}
	return tags, nil
}

// Admin

func (s *Store) upsertAdmin(ctx context.Context, username, passwordHash string) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO admin (username, password_hash) VALUES (?, ?) ON CONFLICT(username) DO UPDATE SET password_hash = ?",
		username, passwordHash, passwordHash)
	return err
}

func (s *Store) findAdmin(ctx context.Context, username string) (*AdminUser, error) {
	var u AdminUser
	err := s.db.QueryRowContext(ctx, "SELECT username, password_hash FROM admin WHERE username = ?", username).Scan(&u.Username, &u.PasswordHash)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// Settings

func (s *Store) getSettings(ctx context.Context) (*BlogSettings, error) {
	var bs BlogSettings
	err := s.db.QueryRowContext(ctx, "SELECT title, subtitle FROM settings WHERE id = 1").Scan(&bs.Title, &bs.Subtitle)
	if err != nil {
		return &BlogSettings{}, nil
	}
	return &bs, nil
}

func (s *Store) upsertSettings(ctx context.Context, bs *BlogSettings) error {
	_, err := s.db.ExecContext(ctx, "UPDATE settings SET title = ?, subtitle = ? WHERE id = 1", bs.Title, bs.Subtitle)
	return err
}

// Scan helpers

func scanPost(r *sql.Row) (*Post, error) {
	var p Post
	var tagsRaw string
	var encrypted int
	var encSalt, encNonce, encCipher string
	var createdAt, updatedAt string

	err := r.Scan(&p.ID, &p.Title, &p.Content, &p.ContentHTML, &p.Summary, &p.Date,
		&tagsRaw, &p.Category, &p.Status, &encrypted,
		&encSalt, &encNonce, &encCipher, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	p.Tags = splitTags(tagsRaw)
	p.Encrypted = encrypted != 0
	if p.Encrypted {
		p.Encryption = &EncryptionData{Salt: encSalt, Nonce: encNonce, Ciphertext: encCipher}
	}
	p.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	p.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &p, nil
}

func scanPosts(rows *sql.Rows) ([]Post, error) {
	var posts []Post
	for rows.Next() {
		var p Post
		var tagsRaw string
		var encrypted int
		var encSalt, encNonce, encCipher string
		var createdAt, updatedAt string

		if err := rows.Scan(&p.ID, &p.Title, &p.Content, &p.ContentHTML, &p.Summary, &p.Date,
			&tagsRaw, &p.Category, &p.Status, &encrypted,
			&encSalt, &encNonce, &encCipher, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		p.Tags = splitTags(tagsRaw)
		p.Encrypted = encrypted != 0
		if p.Encrypted {
			p.Encryption = &EncryptionData{Salt: encSalt, Nonce: encNonce, Ciphertext: encCipher}
		}
		p.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		p.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		posts = append(posts, p)
	}
	if posts == nil {
		posts = []Post{}
	}
	return posts, nil
}

// Helpers

func joinTags(tags []string) string {
	// JSON array format stored as text for LIKE queries
	sb := strings.Builder{}
	sb.WriteString("[")
	for i, t := range tags {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`"` + t + `"`)
	}
	sb.WriteString("]")
	return sb.String()
}

func splitTags(raw string) []string {
	raw = strings.Trim(raw, "[]")
	if raw == "" {
		return []string{}
	}
	parts := strings.Split(raw, ",")
	tags := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		p = strings.Trim(p, `"`)
		if p != "" {
			tags = append(tags, p)
		}
	}
	return tags
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func encryptionFromPost(p *Post) EncryptionData {
	if p.Encryption != nil {
		return *p.Encryption
	}
	return EncryptionData{}
}
