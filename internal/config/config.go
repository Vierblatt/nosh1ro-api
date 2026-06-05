package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	Port          string
	DBPath        string
	JWTSecret     string
	AdminUsername string
	AdminPassword string
	BlogTitle     string
	BlogSubtitle  string
	SMTPHost      string
	SMTPPort      string
	SMTPUsername  string
	SMTPPassword  string
	SMTPFrom      string
	BaseURL       string
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:          env("PORT", "8080"),
		DBPath:        env("DB_PATH", "blog.db"),
		JWTSecret:     env("JWT_SECRET", ""),
		AdminUsername: env("ADMIN_USERNAME", "admin"),
		AdminPassword: env("ADMIN_PASSWORD", ""),
		BlogTitle:     env("BLOG_TITLE", "nosh1ro"),
		BlogSubtitle:  env("BLOG_SUBTITLE", ""),
		SMTPHost:      env("SMTP_HOST", ""),
		SMTPPort:      env("SMTP_PORT", "587"),
		SMTPUsername:  env("SMTP_USERNAME", ""),
		SMTPPassword:  env("SMTP_PASSWORD", ""),
		SMTPFrom:      env("SMTP_FROM", ""),
		BaseURL:       env("BASE_URL", "https://nosh1ro.top"),
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) Validate() error {
	var missing []string
	if c.JWTSecret == "" {
		missing = append(missing, "JWT_SECRET")
	}
	if c.AdminPassword == "" {
		missing = append(missing, "ADMIN_PASSWORD")
	}
	if len(missing) > 0 {
		return fmt.Errorf("required env vars not set: %s", strings.Join(missing, ", "))
	}
	return nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
