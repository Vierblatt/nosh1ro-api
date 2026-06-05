package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"os"
	"testing"

	"golang.org/x/crypto/pbkdf2"
)

func makeTestEncryption(password, plaintext string) *EncryptionData {
	salt := make([]byte, 16)
	io.ReadFull(rand.Reader, salt)
	nonce := make([]byte, 12)
	io.ReadFull(rand.Reader, nonce)
	key := pbkdf2.Key([]byte(password), salt, 100000, 32, sha256.New)
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	ciphertext := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	return &EncryptionData{
		Salt:       base64.StdEncoding.EncodeToString(salt),
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
	}
}

func TestDecryptContent_Success(t *testing.T) {
	enc := makeTestEncryption("password123", "hello world")
	plain, err := decryptContent(enc, "password123")
	if err != nil {
		t.Fatalf("decryptContent: %v", err)
	}
	if plain != "hello world" {
		t.Errorf("plain = %q, want %q", plain, "hello world")
	}
}

func TestDecryptContent_WrongPassword(t *testing.T) {
	enc := makeTestEncryption("password123", "hello world")
	_, err := decryptContent(enc, "wrong")
	if err == nil {
		t.Error("expected error with wrong password")
	}
}

func TestDecryptContent_InvalidSalt(t *testing.T) {
	enc := &EncryptionData{Salt: "!!!invalid!!!", Nonce: "AAAA", Ciphertext: "AAAA"}
	_, err := decryptContent(enc, "pw")
	if err == nil {
		t.Error("expected error with invalid salt")
	}
}

func TestLoadEncryptionJSONField(t *testing.T) {
	os.WriteFile("_test.json", []byte(`{"salt":"abc","nonce":"def","ciphertext":"ghi"}`), 0644)
	defer os.Remove("_test.json")

	if s := loadEncryptionJSONField("_test.json", "salt"); s != "abc" {
		t.Errorf("salt = %q, want %q", s, "abc")
	}
	if s := loadEncryptionJSONField("_test.json", "nonce"); s != "def" {
		t.Errorf("nonce = %q, want %q", s, "def")
	}
	if s := loadEncryptionJSONField("_test.json", "ciphertext"); s != "ghi" {
		t.Errorf("ciphertext = %q, want %q", s, "ghi")
	}
}
