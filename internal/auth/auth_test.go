package auth

import (
	"testing"
)

func TestHashAndCheckPassword(t *testing.T) {
	hash, err := HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("hashPassword: %v", err)
	}
	if hash == "" {
		t.Fatal("expected non-empty hash")
	}
	if hash == "correct-horse-battery-staple" {
		t.Fatal("hash should not equal plaintext")
	}

	if err := CheckPassword(hash, "correct-horse-battery-staple"); err != nil {
		t.Errorf("checkPassword correct: %v", err)
	}
	if err := CheckPassword(hash, "wrong-password"); err == nil {
		t.Error("checkPassword should reject wrong password")
	}
}

func TestHashPasswordDeterministic(t *testing.T) {
	h1, _ := HashPassword("same")
	h2, _ := HashPassword("same")
	if h1 == h2 {
		t.Fatal("bcrypt hashes should include unique salt, got identical hashes")
	}
}

func TestGenerateAndValidateToken(t *testing.T) {
	secret := "test-secret"
	token, err := GenerateToken(secret, "admin")
	if err != nil {
		t.Fatalf("generateToken: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	claims, err := ValidateToken(secret, token)
	if err != nil {
		t.Fatalf("validateToken: %v", err)
	}
	if claims.Username != "admin" {
		t.Errorf("username = %q, want %q", claims.Username, "admin")
	}
}

func TestValidateToken_WrongSecret(t *testing.T) {
	token, _ := GenerateToken("good-secret", "admin")
	_, err := ValidateToken("wrong-secret", token)
	if err == nil {
		t.Error("validateToken should fail with wrong secret")
	}
}

func TestValidateToken_Tampered(t *testing.T) {
	secret := "secret"
	token, _ := GenerateToken(secret, "admin")
	tampered := token + "x"
	_, err := ValidateToken(secret, tampered)
	if err == nil {
		t.Error("validateToken should fail with tampered token")
	}
}
