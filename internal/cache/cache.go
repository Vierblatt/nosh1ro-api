package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

type Client struct {
	rdb *redis.Client
}

func New(url string) (*Client, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}
	rdb := redis.NewClient(opts)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	return &Client{rdb: rdb}, nil
}

func (c *Client) Close() error {
	return c.rdb.Close()
}

func (c *Client) IsReady(ctx context.Context) bool {
	return c.rdb.Ping(ctx).Err() == nil
}

// --- Generic cache ---

func (c *Client) Get(ctx context.Context, key string, dest any) error {
	data, err := c.rdb.Get(ctx, key).Bytes()
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dest)
}

func (c *Client) Set(ctx context.Context, key string, val any, ttl time.Duration) error {
	data, err := json.Marshal(val)
	if err != nil {
		return fmt.Errorf("marshal cache: %w", err)
	}
	return c.rdb.Set(ctx, key, data, ttl).Err()
}

func (c *Client) Del(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	return c.rdb.Del(ctx, keys...).Err()
}

func (c *Client) DeletePattern(ctx context.Context, pattern string) error {
	var cursor uint64
	for {
		keys, next, err := c.rdb.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return fmt.Errorf("scan: %w", err)
		}
		if len(keys) > 0 {
			if err := c.rdb.Del(ctx, keys...).Err(); err != nil {
				return fmt.Errorf("del pattern: %w", err)
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return nil
}

// --- View counts (ZSet) ---

func (c *Client) IncrViews(ctx context.Context, postID string) error {
	return c.rdb.ZIncrBy(ctx, "posts:views", 1, postID).Err()
}

func (c *Client) HotPosts(ctx context.Context, limit int64) ([]string, error) {
	ids, err := c.rdb.ZRevRange(ctx, "posts:hot", 0, limit-1).Result()
	if err != nil {
		return nil, err
	}
	return ids, nil
}

func (c *Client) RefreshHotPosts(ctx context.Context) {
	count, err := c.rdb.ZUnionStore(ctx, "posts:hot", &redis.ZStore{
		Keys:    []string{"posts:views"},
		Weights: []float64{1},
	}).Result()
	if err != nil {
		slog.Error("refresh hot posts", "error", err)
		return
	}
	slog.Debug("hot posts refreshed", "count", count)
}

// --- Rate limiting: sliding window via sorted set ---

func (c *Client) Allow(ctx context.Context, key string, limit int64, window time.Duration) bool {
	now := time.Now().UnixMilli()
	cutoff := now - window.Milliseconds()

	pipe := c.rdb.Pipeline()
	pipe.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%d", cutoff))
	countCmd := pipe.ZCount(ctx, key, fmt.Sprintf("%d", cutoff), "+inf")
	pipe.ZAdd(ctx, key, redis.Z{Score: float64(now), Member: fmt.Sprintf("%d:%d", now, limit)})
	pipe.Expire(ctx, key, window)
	if _, err := pipe.Exec(ctx); err != nil {
		slog.Error("rate limit redis", "error", err)
		return true // allow on error
	}
	return countCmd.Val() < limit
}

// --- Infrastructure ---

func (c *Client) Publish(ctx context.Context, channel string, message any) error {
	data, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("marshal pubsub: %w", err)
	}
	return c.rdb.Publish(ctx, channel, data).Err()
}
