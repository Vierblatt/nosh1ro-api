package main

import "os"

type Config struct {
	Port          string
	MongoURI      string
	DBName        string
	JWTSecret     string
	AdminUsername string
	AdminPassword string
	BlogTitle     string
	BlogSubtitle  string
}

func loadConfig() Config {
	cfg := Config{
		Port:          env("PORT", "8080"),
		MongoURI:      env("MONGO_URI", "mongodb://localhost:27017"),
		DBName:        env("DB_NAME", "nosh1ro_blog"),
		JWTSecret:     env("JWT_SECRET", ""),
		AdminUsername: env("ADMIN_USERNAME", "admin"),
		AdminPassword: env("ADMIN_PASSWORD", ""),
		BlogTitle:     env("BLOG_TITLE", "nosh1ro"),
		BlogSubtitle:  env("BLOG_SUBTITLE", ""),
	}
	if cfg.JWTSecret == "" {
		panic("JWT_SECRET environment variable is required")
	}
	if cfg.AdminPassword == "" {
		panic("ADMIN_PASSWORD environment variable is required")
	}
	return cfg
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
