package main

import "time"

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

type AdminUser struct {
	Username     string
	PasswordHash string
}

type BlogSettings struct {
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
}
