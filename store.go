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
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("sqlite open: %w", err)
	}
	db.SetMaxOpenConns(1)
	return &Store{db: db}, nil
}

func (s *Store) initSchema(ctx context.Context) error {
	schema := `
	CREATE TABLE IF NOT EXISTS posts (
		id           TEXT PRIMARY KEY,
		title        TEXT NOT NULL,
		content      TEXT NOT NULL DEFAULT '',
		content_html TEXT NOT NULL DEFAULT '',
		summary      TEXT NOT NULL DEFAULT '',
		date         TEXT NOT NULL,
		category     TEXT NOT NULL DEFAULT '',
		status       TEXT NOT NULL DEFAULT 'draft',
		encrypted    INTEGER NOT NULL DEFAULT 0,
		enc_salt     TEXT NOT NULL DEFAULT '',
		enc_nonce    TEXT NOT NULL DEFAULT '',
		enc_cipher   TEXT NOT NULL DEFAULT '',
		created_at   TEXT NOT NULL,
		updated_at   TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_posts_date ON posts(date DESC);
	CREATE INDEX IF NOT EXISTS idx_posts_status ON posts(status);

	CREATE TABLE IF NOT EXISTS tags (
		name TEXT PRIMARY KEY
	);

	CREATE TABLE IF NOT EXISTS post_tags (
		post_id TEXT NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
		tag     TEXT NOT NULL REFERENCES tags(name) ON DELETE CASCADE,
		PRIMARY KEY (post_id, tag)
	);
	CREATE INDEX IF NOT EXISTS idx_post_tags_tag ON post_tags(tag);

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

// --- Post CRUD ---

func (s *Store) findPosts(ctx context.Context, f PostFilter, page, size int64) (*PostListResult, error) {
	var conditions []string
	var args []any

	if f.Status != "" {
		conditions = append(conditions, "p.status = ?")
		args = append(args, f.Status)
	}
	if f.Category != "" {
		conditions = append(conditions, "p.category = ?")
		args = append(args, f.Category)
	}
	if f.Tag != "" {
		conditions = append(conditions, `p.id IN (SELECT post_id FROM post_tags WHERE tag = ?)`)
		args = append(args, f.Tag)
	}
	if f.Search != "" {
		conditions = append(conditions, "(p.title LIKE ? OR p.content LIKE ?)")
		q := "%" + f.Search + "%"
		args = append(args, q, q)
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	var total int64
	countSQL := "SELECT COUNT(*) FROM posts p " + where
	if err := s.db.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count posts: %w", err)
	}

	offset := (page - 1) * size
	selectSQL := "SELECT p.id, p.title, p.content, p.content_html, p.summary, p.date, p.category, p.status, p.encrypted, p.enc_salt, p.enc_nonce, p.enc_cipher, p.created_at, p.updated_at FROM posts p " + where + " ORDER BY p.date DESC LIMIT ? OFFSET ?"
	args = append(args, size, offset)

	rows, err := s.db.QueryContext(ctx, selectSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("query posts: %w", err)
	}
	defer rows.Close()

	var postList []Post
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			return nil, fmt.Errorf("scan post: %w", err)
		}
		tags, err := s.postTags(ctx, p.ID)
		if err != nil {
			return nil, fmt.Errorf("get tags: %w", err)
		}
		p.Tags = tags
		postList = append(postList, *p)
	}
	if postList == nil {
		postList = []Post{}
	}
	return &PostListResult{Posts: postList, Total: total, Page: page, Size: size}, nil
}

func (s *Store) countPosts(ctx context.Context, f PostFilter) (int64, error) {
	var conditions []string
	var args []any
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
		"SELECT id, title, content, content_html, summary, date, category, status, encrypted, enc_salt, enc_nonce, enc_cipher, created_at, updated_at FROM posts WHERE id = ?", id)
	p, err := scanPost(row)
	if err != nil {
		return nil, err
	}
	tags, err := s.postTags(ctx, p.ID)
	if err != nil {
		return nil, fmt.Errorf("get tags: %w", err)
	}
	p.Tags = tags
	return p, nil
}

func (s *Store) insertPost(ctx context.Context, p *Post) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	enc := p.encryptionData()
	_, err = tx.ExecContext(ctx,
		"INSERT INTO posts (id, title, content, content_html, summary, date, category, status, encrypted, enc_salt, enc_nonce, enc_cipher, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
		p.ID, p.Title, p.Content, p.ContentHTML, p.Summary, p.Date, p.Category, p.Status, boolToInt(p.Encrypted),
		enc.Salt, enc.Nonce, enc.Ciphertext, p.CreatedAt.Format(time.RFC3339), p.UpdatedAt.Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("insert post: %w", err)
	}
	if err := s.setPostTagsTx(ctx, tx, p.ID, p.Tags); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) replacePost(ctx context.Context, p *Post) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	enc := p.encryptionData()
	_, err = tx.ExecContext(ctx,
		"UPDATE posts SET title=?, content=?, content_html=?, summary=?, date=?, category=?, status=?, encrypted=?, enc_salt=?, enc_nonce=?, enc_cipher=?, updated_at=? WHERE id=?",
		p.Title, p.Content, p.ContentHTML, p.Summary, p.Date, p.Category, p.Status, boolToInt(p.Encrypted),
		enc.Salt, enc.Nonce, enc.Ciphertext, p.UpdatedAt.Format(time.RFC3339), p.ID)
	if err != nil {
		return fmt.Errorf("update post: %w", err)
	}
	if err := s.setPostTagsTx(ctx, tx, p.ID, p.Tags); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) deletePost(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM posts WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete post: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) postExists(ctx context.Context, id string) bool {
	var exists int
	err := s.db.QueryRowContext(ctx, "SELECT 1 FROM posts WHERE id = ?", id).Scan(&exists)
	return err == nil && exists == 1
}

// --- Tags ---

func (s *Store) allTags(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT name FROM tags ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("query tags: %w", err)
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, fmt.Errorf("scan tag: %w", err)
		}
		tags = append(tags, t)
	}
	if tags == nil {
		tags = []string{}
	}
	return tags, nil
}

func (s *Store) postTags(ctx context.Context, postID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT tag FROM post_tags WHERE post_id = ? ORDER BY tag", postID)
	if err != nil {
		return nil, fmt.Errorf("query post tags: %w", err)
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, fmt.Errorf("scan tag: %w", err)
		}
		tags = append(tags, t)
	}
	if tags == nil {
		tags = []string{}
	}
	return tags, nil
}

func (s *Store) setPostTagsTx(ctx context.Context, tx *sql.Tx, postID string, tags []string) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM post_tags WHERE post_id = ?", postID); err != nil {
		return fmt.Errorf("clear tags: %w", err)
	}
	for _, t := range tags {
		if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO tags (name) VALUES (?)", t); err != nil {
			return fmt.Errorf("insert tag %q: %w", t, err)
		}
		if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO post_tags (post_id, tag) VALUES (?, ?)", postID, t); err != nil {
			return fmt.Errorf("link tag %q: %w", t, err)
		}
	}
	return nil
}

// --- Admin ---

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

// --- Settings ---

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

// --- Scan helpers ---

// scanner abstracts sql.Row and sql.Rows for a shared scanPost helper.
type scanner interface {
	Scan(dest ...any) error
}

func scanPost(s scanner) (*Post, error) {
	var p Post
	var encrypted int
	var encSalt, encNonce, encCipher string
	var createdAt, updatedAt string

	err := s.Scan(&p.ID, &p.Title, &p.Content, &p.ContentHTML, &p.Summary, &p.Date,
		&p.Category, &p.Status, &encrypted,
		&encSalt, &encNonce, &encCipher, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	p.Encrypted = encrypted != 0
	if p.Encrypted {
		p.Encryption = &EncryptionData{Salt: encSalt, Nonce: encNonce, Ciphertext: encCipher}
	}
	p.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	p.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &p, nil
}

// --- Helpers ---

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (p *Post) encryptionData() EncryptionData {
	if p.Encryption != nil {
		return *p.Encryption
	}
	return EncryptionData{}
}
