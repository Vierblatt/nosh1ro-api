package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"

	"golang.org/x/crypto/pbkdf2"
)

func decryptContent(enc *EncryptionData, password string) (string, error) {
	salt, err := base64.StdEncoding.DecodeString(enc.Salt)
	if err != nil {
		return "", fmt.Errorf("invalid salt: %w", err)
	}
	nonce, err := base64.StdEncoding.DecodeString(enc.Nonce)
	if err != nil {
		return "", fmt.Errorf("invalid nonce: %w", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(enc.Ciphertext)
	if err != nil {
		return "", fmt.Errorf("invalid ciphertext: %w", err)
	}

	key := pbkdf2.Key([]byte(password), salt, 100000, 32, sha256.New)

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("aes init: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("gcm init: %w", err)
	}
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt failed")
	}
	return string(plain), nil
}

func loadEncryptionJSONField(path, field string) string {
	var m map[string]string
	data, err := os.ReadFile(path)
	if err != nil {
		panic("failed to read " + path + ": " + err.Error())
	}
	if err := json.Unmarshal(data, &m); err != nil {
		panic("failed to parse " + path + ": " + err.Error())
	}
	return m[field]
}
