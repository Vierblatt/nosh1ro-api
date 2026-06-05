package email

import (
	"fmt"
	"log/slog"
	"net/smtp"
)

type Config struct {
	Host     string
	Port     string
	Username string
	Password string
	From     string
	BaseURL  string
}

func (c *Config) Enabled() bool {
	return c.Host != "" && c.Port != ""
}

func (c *Config) SendVerification(to, username, token string) {
	if !c.Enabled() {
		slog.Warn("email not configured, cannot send verification")
		return
	}

	link := fmt.Sprintf("%s/verify?token=%s", c.BaseURL, token)
	body := fmt.Sprintf("From: %s\r\n"+
		"To: %s\r\n"+
		"Subject: 验证邮箱 — nosh1ro\r\n"+
		"Content-Type: text/plain; charset=UTF-8\r\n"+
		"\r\n"+
		"你好 %s，\r\n\r\n"+
		"请点击以下链接验证你的邮箱：\r\n"+
		"%s\r\n\r\n"+
		"此链接 24 小时内有效。\r\n", c.From, to, username, link)

	go func() {
		addr := fmt.Sprintf("%s:%s", c.Host, c.Port)
		var auth smtp.Auth
		if c.Username != "" {
			auth = smtp.PlainAuth("", c.Username, c.Password, c.Host)
		}
		err := smtp.SendMail(addr, auth, c.From, []string{to}, []byte(body))
		if err != nil {
			slog.Error("send verification email", "to", to, "error", err)
		} else {
			slog.Info("verification email sent", "to", to)
		}
	}()
}
