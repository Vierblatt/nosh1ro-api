package auth

import (
	"fmt"
	"time"
	"unicode"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type Claims struct {
	Username string `json:"username"`
	Role     string `json:"role"`
	Purpose  string `json:"purpose,omitempty"`
	jwt.RegisteredClaims
}

func GenerateToken(secret, username, role string) (string, error) {
	return generateToken(secret, username, role, "auth", 72*time.Hour)
}

func GenerateVerificationToken(secret, username string) (string, error) {
	return generateToken(secret, username, "", "verify", 24*time.Hour)
}

func generateToken(secret, username, role, purpose string, ttl time.Duration) (string, error) {
	claims := Claims{
		Username: username,
		Role:     role,
		Purpose:  purpose,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

func ValidateToken(secret, tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{},
		func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			return []byte(secret), nil
		})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	return claims, nil
}

func HashPassword(pw string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(bytes), err
}

func CheckPassword(hash, pw string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw))
}

func ValidatePassword(pw string) error {
	if len(pw) < 8 {
		return fmt.Errorf("密码至少需要8个字符")
	}
	var hasLetter, hasDigit bool
	for _, c := range pw {
		if unicode.IsLetter(c) {
			hasLetter = true
		}
		if unicode.IsDigit(c) {
			hasDigit = true
		}
	}
	if !hasLetter || !hasDigit {
		return fmt.Errorf("密码必须包含字母和数字")
	}
	return nil
}
