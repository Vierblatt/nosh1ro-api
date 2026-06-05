package main

import "time"

type EncryptionData struct {
	Salt       string `json:"salt" bson:"salt"`
	Nonce      string `json:"nonce" bson:"nonce"`
	Ciphertext string `json:"ciphertext" bson:"ciphertext"`
}

type Post struct {
	ID          string          `json:"id" bson:"_id"`
	Title       string          `json:"title" bson:"title"`
	Content     string          `json:"content" bson:"content"`
	ContentHTML string          `json:"content_html,omitempty" bson:"content_html"`
	Summary     string          `json:"summary,omitempty" bson:"summary"`
	Date        string          `json:"date" bson:"date"`
	Tags        []string        `json:"tags,omitempty" bson:"tags"`
	Category    string          `json:"category,omitempty" bson:"category"`
	Status      string          `json:"status" bson:"status"`
	Encrypted   bool            `json:"encrypted,omitempty" bson:"encrypted"`
	Encryption  *EncryptionData `json:"encryption,omitempty" bson:"encryption,omitempty"`
	CreatedAt   time.Time       `json:"created_at" bson:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at" bson:"updated_at"`
}

type AdminUser struct {
	Username     string `bson:"_id"`
	PasswordHash string `bson:"password_hash"`
}

type BlogSettings struct {
	Title    string `json:"title" bson:"title"`
	Subtitle string `json:"subtitle" bson:"subtitle"`
}
