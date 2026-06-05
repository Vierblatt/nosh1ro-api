package main

import (
	"testing"
)

func TestHashAndCheckPassword(t *testing.T) {
	hash, err := hashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("hashPassword: %v", err)
	}
	if hash == "" {
		t.Fatal("expected non-empty hash")
	}
	if hash == "correct-horse-battery-staple" {
		t.Fatal("hash should not equal plaintext")
	}

	if err := checkPassword(hash, "correct-horse-battery-staple"); err != nil {
		t.Errorf("checkPassword correct: %v", err)
	}
	if err := checkPassword(hash, "wrong-password"); err == nil {
		t.Error("checkPassword should reject wrong password")
	}
}

func TestHashPasswordDeterministic(t *testing.T) {
	h1, _ := hashPassword("same")
	h2, _ := hashPassword("same")
	if h1 == h2 {
		t.Fatal("bcrypt hashes should include unique salt, got identical hashes")
	}
}

func TestGenerateAndValidateToken(t *testing.T) {
	secret := "test-secret"
	token, err := generateToken(secret, "admin")
	if err != nil {
		t.Fatalf("generateToken: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	claims, err := validateToken(secret, token)
	if err != nil {
		t.Fatalf("validateToken: %v", err)
	}
	if claims.Username != "admin" {
		t.Errorf("username = %q, want %q", claims.Username, "admin")
	}
}

func TestValidateToken_WrongSecret(t *testing.T) {
	token, _ := generateToken("good-secret", "admin")
	_, err := validateToken("wrong-secret", token)
	if err == nil {
		t.Error("validateToken should fail with wrong secret")
	}
}

func TestValidateToken_Tampered(t *testing.T) {
	secret := "secret"
	token, _ := generateToken(secret, "admin")
	tampered := token + "x"
	_, err := validateToken(secret, tampered)
	if err == nil {
		t.Error("validateToken should fail with tampered token")
	}
}
