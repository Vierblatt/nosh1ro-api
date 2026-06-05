package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/Vierblatt/nosh1ro-api/internal/model"
	_ "github.com/go-sql-driver/mysql"
	_ "modernc.org/sqlite"
)

type Store struct {
	db      *sql.DB
	dialect string // "mysql" or "sqlite"
}

func New(dbType, dsn string) (*Store, error) {
	var drv string
	switch dbType {
	case "mysql":
		drv = "mysql"
	case "sqlite":
		drv = "sqlite"
		dsn += "?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on"
	default:
		return nil, fmt.Errorf("unsupported DB_TYPE: %s (use mysql or sqlite)", dbType)
	}
	db, err := sql.Open(drv, dsn)
	if err != nil {
		return nil, fmt.Errorf("%s open: %w", dbType, err)
	}
	if dbType == "sqlite" {
		db.SetMaxOpenConns(1)
	}
	return &Store{db: db, dialect: dbType}, nil
}

func (s *Store) InitSchema(ctx context.Context) error {
	if s.dialect == "mysql" {
		return s.initMySQLSchema(ctx)
	}
	return s.initSQLiteSchema(ctx)
}

func (s *Store) initMySQLSchema(ctx context.Context) error {
	schema := `
	CREATE TABLE IF NOT EXISTS posts (
		id           VARCHAR(255) PRIMARY KEY,
		title        TEXT NOT NULL,
		content      TEXT NOT NULL,
		content_html TEXT NOT NULL,
		summary      TEXT NOT NULL,
		date         VARCHAR(255) NOT NULL,
		category     VARCHAR(255) NOT NULL DEFAULT '',
		status       VARCHAR(50) NOT NULL DEFAULT 'draft',
		encrypted    TINYINT(1) NOT NULL DEFAULT 0,
		enc_salt     TEXT NOT NULL,
		enc_nonce    TEXT NOT NULL,
		enc_cipher   MEDIUMTEXT NOT NULL,
		created_at   VARCHAR(255) NOT NULL,
		updated_at   VARCHAR(255) NOT NULL
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

	CREATE TABLE IF NOT EXISTS tags (
		name VARCHAR(255) PRIMARY KEY
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

	CREATE TABLE IF NOT EXISTS post_tags (
		post_id VARCHAR(255) NOT NULL,
		tag     VARCHAR(255) NOT NULL,
		PRIMARY KEY (post_id, tag),
		FOREIGN KEY (post_id) REFERENCES posts(id) ON DELETE CASCADE,
		FOREIGN KEY (tag) REFERENCES tags(name) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

	CREATE TABLE IF NOT EXISTS admin (
		username      VARCHAR(255) PRIMARY KEY,
		password_hash TEXT NOT NULL,
		email         VARCHAR(255) NOT NULL DEFAULT '',
		verified      TINYINT(1) NOT NULL DEFAULT 0,
		verify_token  TEXT NOT NULL,
		role          VARCHAR(50) NOT NULL DEFAULT 'user',
		created_at    TEXT NOT NULL
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

	CREATE TABLE IF NOT EXISTS settings (
		id       INT AUTO_INCREMENT PRIMARY KEY,
		title    TEXT NOT NULL,
		subtitle TEXT NOT NULL
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
	INSERT IGNORE INTO settings (id, title, subtitle) VALUES (1, '', '');
	`
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return err
	}

	indexes := []string{
		"CREATE INDEX idx_posts_date ON posts(date DESC)",
		"CREATE INDEX idx_posts_status ON posts(status)",
		"CREATE INDEX idx_post_tags_tag ON post_tags(tag)",
	}
	for _, idx := range indexes {
		if _, err := s.db.ExecContext(ctx, idx); err != nil {
			if isDupKeyErr(err) {
				continue
			}
			return err
		}
	}
	return nil
}

func (s *Store) initSQLiteSchema(ctx context.Context) error {
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
		password_hash TEXT NOT NULL,
		email         TEXT NOT NULL DEFAULT '',
		verified      INTEGER NOT NULL DEFAULT 0,
		verify_token  TEXT NOT NULL DEFAULT '',
		role          TEXT NOT NULL DEFAULT 'user',
		created_at    TEXT NOT NULL DEFAULT ''
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

func isDupKeyErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "Error 1061")
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// --- Post CRUD ---

func (s *Store) FindPosts(ctx context.Context, f model.PostFilter, page, size int64) (*model.PostListResult, error) {
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

	var posts []model.Post
	var ids []string
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			return nil, fmt.Errorf("scan post: %w", err)
		}
		posts = append(posts, *p)
		ids = append(ids, p.ID)
	}
	rows.Close()

	tagMap, err := s.batchPostTags(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("batch tags: %w", err)
	}
	for i := range posts {
		posts[i].Tags = tagMap[posts[i].ID]
	}
	if posts == nil {
		posts = []model.Post{}
	}
	return &model.PostListResult{Posts: posts, Total: total, Page: page, Size: size}, nil
}

func (s *Store) CountPosts(ctx context.Context, f model.PostFilter) (int64, error) {
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

func (s *Store) FindPost(ctx context.Context, id string) (*model.Post, error) {
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

func (s *Store) InsertPost(ctx context.Context, p *model.Post) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	enc := p.EncryptionData()
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

func (s *Store) ReplacePost(ctx context.Context, p *model.Post) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	enc := p.EncryptionData()
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

func (s *Store) DeletePost(ctx context.Context, id string) error {
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

func (s *Store) PostExists(ctx context.Context, id string) bool {
	var exists int
	err := s.db.QueryRowContext(ctx, "SELECT 1 FROM posts WHERE id = ?", id).Scan(&exists)
	return err == nil && exists == 1
}

// --- Tags ---

func (s *Store) AllTags(ctx context.Context) ([]string, error) {
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

// --- Admin ---

func (s *Store) MigrateAdminSchema(ctx context.Context) error {
	existing := make(map[string]bool)
	if s.dialect == "mysql" {
		rows, err := s.db.QueryContext(ctx, "SHOW COLUMNS FROM admin")
		if err != nil {
			return fmt.Errorf("show columns: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var field, typ, null, key, extra string
			var dflt sql.NullString
			if err := rows.Scan(&field, &typ, &null, &key, &dflt, &extra); err != nil {
				return fmt.Errorf("scan column: %w", err)
			}
			existing[field] = true
		}
	} else {
		rows, err := s.db.QueryContext(ctx, "PRAGMA table_info(admin)")
		if err != nil {
			return fmt.Errorf("pragma table_info: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var cid int
			var name, typ string
			var notNull int
			var dflt sql.NullString
			var pk int
			if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
				return fmt.Errorf("scan pragma: %w", err)
			}
			existing[name] = true
		}
	}

	newCols := []struct{ name, def, mysqlDef string }{
		{"email", "TEXT NOT NULL DEFAULT ''", "VARCHAR(255) NOT NULL DEFAULT ''"},
		{"verified", "INTEGER NOT NULL DEFAULT 0", "TINYINT(1) NOT NULL DEFAULT 0"},
		{"verify_token", "TEXT NOT NULL DEFAULT ''", "TEXT NOT NULL"},
		{"created_at", "TEXT NOT NULL DEFAULT ''", "TEXT NOT NULL"},
		{"role", "TEXT NOT NULL DEFAULT 'user'", "VARCHAR(50) NOT NULL DEFAULT 'user'"},
	}

	for _, col := range newCols {
		if !existing[col.name] {
			def := col.def
			if s.dialect == "mysql" {
				def = col.mysqlDef
			}
			_, err := s.db.ExecContext(ctx, "ALTER TABLE admin ADD COLUMN "+col.name+" "+def)
			if err != nil {
				return fmt.Errorf("add column %s: %w", col.name, err)
			}
		}
	}

	_, err := s.db.ExecContext(ctx, "UPDATE admin SET role = 'admin' WHERE role = 'user' AND email = ''")
	return err
}

func (s *Store) UpsertAdmin(ctx context.Context, username, passwordHash string) error {
	if s.dialect == "mysql" {
		_, err := s.db.ExecContext(ctx,
			"INSERT INTO admin (username, password_hash, verified, verify_token, role, created_at) VALUES (?, ?, 1, '', 'admin', ?) ON DUPLICATE KEY UPDATE password_hash = ?",
			username, passwordHash, time.Now().Format(time.RFC3339), passwordHash)
		return err
	}
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO admin (username, password_hash, verified, verify_token, role, created_at) VALUES (?, ?, 1, '', 'admin', ?) ON CONFLICT(username) DO UPDATE SET password_hash = ?",
		username, passwordHash, time.Now().Format(time.RFC3339), passwordHash)
	return err
}

func (s *Store) FindAdmin(ctx context.Context, username string) (*model.AdminUser, error) {
	var u model.AdminUser
	var createdAt string
	err := s.db.QueryRowContext(ctx, "SELECT username, password_hash, email, verified, verify_token, role, created_at FROM admin WHERE username = ?", username).
		Scan(&u.Username, &u.PasswordHash, &u.Email, &u.Verified, &u.VerifyToken, &u.Role, &createdAt)
	if err != nil {
		return nil, err
	}
	u.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &u, nil
}

func (s *Store) FindAdminByEmail(ctx context.Context, email string) (*model.AdminUser, error) {
	var u model.AdminUser
	var createdAt string
	err := s.db.QueryRowContext(ctx, "SELECT username, password_hash, email, verified, verify_token, role, created_at FROM admin WHERE email = ?", email).
		Scan(&u.Username, &u.PasswordHash, &u.Email, &u.Verified, &u.VerifyToken, &u.Role, &createdAt)
	if err != nil {
		return nil, err
	}
	u.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &u, nil
}

func (s *Store) FindAdminByVerifyToken(ctx context.Context, token string) (*model.AdminUser, error) {
	var u model.AdminUser
	var createdAt string
	err := s.db.QueryRowContext(ctx, "SELECT username, password_hash, email, verified, verify_token, role, created_at FROM admin WHERE verify_token = ?", token).
		Scan(&u.Username, &u.PasswordHash, &u.Email, &u.Verified, &u.VerifyToken, &u.Role, &createdAt)
	if err != nil {
		return nil, err
	}
	u.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &u, nil
}

func (s *Store) CountAdmins(ctx context.Context) (int64, error) {
	var count int64
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM admin").Scan(&count)
	return count, err
}

func (s *Store) CreateAdmin(ctx context.Context, u *model.AdminUser) error {
	role := u.Role
	if role == "" {
		role = "user"
	}
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO admin (username, password_hash, email, verified, verify_token, role, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		u.Username, u.PasswordHash, u.Email, boolToInt(u.Verified), u.VerifyToken, role, u.CreatedAt.Format(time.RFC3339))
	return err
}

func (s *Store) MarkVerified(ctx context.Context, username string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE admin SET verified = 1, verify_token = '' WHERE username = ?", username)
	return err
}

func (s *Store) SetVerifyToken(ctx context.Context, username, token string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE admin SET verify_token = ? WHERE username = ?", token, username)
	return err
}

// --- Settings ---

func (s *Store) GetSettings(ctx context.Context) (*model.BlogSettings, error) {
	var bs model.BlogSettings
	err := s.db.QueryRowContext(ctx, "SELECT title, subtitle FROM settings WHERE id = 1").Scan(&bs.Title, &bs.Subtitle)
	if err != nil {
		return &model.BlogSettings{}, nil
	}
	return &bs, nil
}

func (s *Store) UpsertSettings(ctx context.Context, bs *model.BlogSettings) error {
	_, err := s.db.ExecContext(ctx, "UPDATE settings SET title = ?, subtitle = ? WHERE id = 1", bs.Title, bs.Subtitle)
	return err
}

// --- internal helpers ---

type scanner interface {
	Scan(dest ...any) error
}

func scanPost(s scanner) (*model.Post, error) {
	var p model.Post
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
		p.Encryption = &model.EncryptionData{Salt: encSalt, Nonce: encNonce, Ciphertext: encCipher}
	}
	p.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	p.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &p, nil
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

func (s *Store) batchPostTags(ctx context.Context, ids []string) (map[string][]string, error) {
	if len(ids) == 0 {
		return map[string][]string{}, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	query := "SELECT post_id, tag FROM post_tags WHERE post_id IN (" + strings.Join(placeholders, ",") + ") ORDER BY tag"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("batch post tags: %w", err)
	}
	defer rows.Close()

	m := make(map[string][]string)
	for rows.Next() {
		var postID, tag string
		if err := rows.Scan(&postID, &tag); err != nil {
			return nil, fmt.Errorf("scan tag: %w", err)
		}
		m[postID] = append(m[postID], tag)
	}
	return m, nil
}

func (s *Store) setPostTagsTx(ctx context.Context, tx *sql.Tx, postID string, tags []string) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM post_tags WHERE post_id = ?", postID); err != nil {
		return fmt.Errorf("clear tags: %w", err)
	}
	ignoreTag := "INSERT OR IGNORE INTO tags (name) VALUES (?)"
	ignoreLink := "INSERT OR IGNORE INTO post_tags (post_id, tag) VALUES (?, ?)"
	if s.dialect == "mysql" {
		ignoreTag = "INSERT IGNORE INTO tags (name) VALUES (?)"
		ignoreLink = "INSERT IGNORE INTO post_tags (post_id, tag) VALUES (?, ?)"
	}
	for _, t := range tags {
		if _, err := tx.ExecContext(ctx, ignoreTag, t); err != nil {
			return fmt.Errorf("insert tag %q: %w", t, err)
		}
		if _, err := tx.ExecContext(ctx, ignoreLink, postID, t); err != nil {
			return fmt.Errorf("link tag %q: %w", t, err)
		}
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
