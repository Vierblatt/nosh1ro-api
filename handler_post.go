package main

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func handleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func handlePosts(store *Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		page, _ := strconv.ParseInt(c.DefaultQuery("page", "1"), 10, 64)
		size, _ := strconv.ParseInt(c.DefaultQuery("size", "10"), 10, 64)
		if page < 1 {
			page = 1
		}
		if size < 1 || size > 50 {
			size = 10
		}

		filter := PostFilter{
			Status:   "published",
			Tag:      c.Query("tag"),
			Category: c.Query("category"),
			Search:   c.Query("q"),
		}

		result, err := store.findPosts(c.Request.Context(), filter, page, size)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch posts"})
			return
		}

		// Strip content for list view; only include summary
		items := make([]gin.H, 0, len(result.Posts))
		for _, p := range result.Posts {
			item := gin.H{
				"id":       p.ID,
				"date":     p.Date,
				"title":    p.Title,
				"summary":  p.Summary,
				"tags":     p.Tags,
				"category": p.Category,
			}
			if p.Encrypted {
				item["encrypted"] = true
				item["encryption"] = gin.H{
					"salt":       p.Encryption.Salt,
					"nonce":      p.Encryption.Nonce,
					"ciphertext": p.Encryption.Ciphertext,
				}
			}
			items = append(items, item)
		}

		c.JSON(http.StatusOK, gin.H{
			"posts": items,
			"total": result.Total,
			"page":  result.Page,
			"size":  result.Size,
		})
	}
}

func handlePostDetail(store *Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		p, err := store.findPost(c.Request.Context(), id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "post not found"})
			return
		}
		if p.Status != "published" {
			c.JSON(http.StatusNotFound, gin.H{"error": "post not found"})
			return
		}

		resp := gin.H{
			"id":           p.ID,
			"date":         p.Date,
			"title":        p.Title,
			"content_html": p.ContentHTML,
			"tags":         p.Tags,
			"category":     p.Category,
		}
		if p.Encrypted {
			resp["encrypted"] = true
			resp["encryption"] = gin.H{
				"salt":       p.Encryption.Salt,
				"nonce":      p.Encryption.Nonce,
				"ciphertext": p.Encryption.Ciphertext,
			}
		} else {
			resp["content"] = p.Content
		}

		c.JSON(http.StatusOK, resp)
	}
}

func handleVerify(store *Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		p, err := store.findPost(c.Request.Context(), id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "post not found"})
			return
		}
		if !p.Encrypted {
			c.JSON(http.StatusBadRequest, gin.H{"error": "post is not encrypted"})
			return
		}

		var req struct {
			Password string `json:"password"`
		}
		if err := c.ShouldBindJSON(&req); err != nil || req.Password == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "password required"})
			return
		}

		content, err := decryptContent(p.Encryption, req.Password)
		if err != nil {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"content": content})
	}
}

func handleTags(store *Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		tags, err := store.distinctTags(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch tags"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"tags": tags})
	}
}
