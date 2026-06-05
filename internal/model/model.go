package model

import "time"

// PostStatus represents the publication status of a post.
type PostStatus string

const (
	StatusDraft     PostStatus = "draft"
	StatusPublished PostStatus = "published"
)

func (s PostStatus) IsValid() bool {
	return s == StatusDraft || s == StatusPublished
}

type EncryptionData struct {
	Salt       string `json:"salt"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

type Post struct {
	ID          string          `json:"id"`
	Title       string          `json:"title"`
	Content     string          `json:"content"`
	ContentHTML string          `json:"content_html,omitempty"`
	Summary     string          `json:"summary,omitempty"`
	Date        string          `json:"date"`
	Tags        []string        `json:"tags,omitempty"`
	Category    string          `json:"category,omitempty"`
	Status      string          `json:"status"`
	Encrypted   bool            `json:"encrypted,omitempty"`
	Encryption  *EncryptionData `json:"encryption,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

func (p *Post) EncryptionData() EncryptionData {
	if p.Encryption != nil {
		return *p.Encryption
	}
	return EncryptionData{}
}

type AdminUser struct {
	Username     string
	PasswordHash string
}

type BlogSettings struct {
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
}

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
