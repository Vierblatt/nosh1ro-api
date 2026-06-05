package es

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	"github.com/Vierblatt/nosh1ro-api/internal/model"
	"github.com/elastic/go-elasticsearch/v8"
)

const indexName = "posts"

type Client struct {
	es *elasticsearch.Client
}

func New(url string) (*Client, error) {
	cfg := elasticsearch.Config{Addresses: []string{url}}
	es, err := elasticsearch.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("es client: %w", err)
	}
	if _, err := es.Info(); err != nil {
		return nil, fmt.Errorf("es ping: %w", err)
	}
	return &Client{es: es}, nil
}

func (c *Client) EnsureIndex(ctx context.Context) error {
	mapping := map[string]any{
		"settings": map[string]any{
			"number_of_shards":   1,
			"number_of_replicas": 0,
		},
		"mappings": map[string]any{
			"properties": map[string]any{
				"title": map[string]any{
					"type":            "text",
					"analyzer":        "ik_max_word",
					"search_analyzer": "ik_smart",
				},
				"content": map[string]any{
					"type":            "text",
					"analyzer":        "ik_max_word",
					"search_analyzer": "ik_smart",
				},
				"summary": map[string]any{
					"type":     "text",
					"analyzer": "ik_smart",
				},
				"category": map[string]any{"type": "keyword"},
				"tags":     map[string]any{"type": "keyword"},
				"date":     map[string]any{"type": "date", "format": "yyyy-MM-dd"},
				"status":   map[string]any{"type": "keyword"},
			},
		},
	}

	res, err := c.es.Indices.Exists([]string{indexName})
	if err != nil {
		return fmt.Errorf("check index: %w", err)
	}
	res.Body.Close()
	if res.StatusCode == 200 {
		return nil
	}

	body, _ := json.Marshal(mapping)
	res, err = c.es.Indices.Create(indexName, c.es.Indices.Create.WithBody(bytes.NewReader(body)))
	if err != nil {
		return fmt.Errorf("create index: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		b, _ := io.ReadAll(res.Body)
		return fmt.Errorf("create index failed: %s", string(b))
	}
	slog.Info("es index created", "index", indexName)
	return nil
}

func (c *Client) IndexPost(ctx context.Context, p *model.Post) error {
	doc := postDoc(p)
	body, _ := json.Marshal(doc)
	res, err := c.es.Index(indexName, bytes.NewReader(body), c.es.Index.WithDocumentID(p.ID))
	if err != nil {
		return fmt.Errorf("index post %s: %w", p.ID, err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		b, _ := io.ReadAll(res.Body)
		return fmt.Errorf("index post %s failed: %s", p.ID, string(b))
	}
	return nil
}

func (c *Client) UpdatePost(ctx context.Context, p *model.Post) error {
	return c.IndexPost(ctx, p)
}

func (c *Client) DeletePost(ctx context.Context, id string) error {
	res, err := c.es.Delete(indexName, id)
	if err != nil {
		return fmt.Errorf("delete post %s from es: %w", id, err)
	}
	defer res.Body.Close()
	if res.StatusCode == 404 {
		return nil
	}
	if res.StatusCode >= 400 {
		b, _ := io.ReadAll(res.Body)
		return fmt.Errorf("delete post %s from es failed: %s", id, string(b))
	}
	return nil
}

func (c *Client) BulkIndex(ctx context.Context, posts []model.Post) error {
	var buf bytes.Buffer
	for _, p := range posts {
		meta := map[string]any{"index": map[string]any{"_index": indexName, "_id": p.ID}}
		metaLine, _ := json.Marshal(meta)
		docLine, _ := json.Marshal(postDoc(&p))
		buf.Write(metaLine)
		buf.WriteByte('\n')
		buf.Write(docLine)
		buf.WriteByte('\n')
	}
	res, err := c.es.Bulk(bytes.NewReader(buf.Bytes()), c.es.Bulk.WithRefresh("true"))
	if err != nil {
		return fmt.Errorf("bulk index: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		b, _ := io.ReadAll(res.Body)
		return fmt.Errorf("bulk index failed: %s", string(b))
	}
	slog.Info("es bulk indexed", "count", len(posts))
	return nil
}

type SearchResult struct {
	Total        int64                  `json:"total"`
	Posts        []SearchHit            `json:"posts"`
	Aggregations map[string][]AggBucket `json:"aggregations"`
}

type SearchHit struct {
	ID       string            `json:"id"`
	Title    string            `json:"title"`
	Summary  string            `json:"summary"`
	Date     string            `json:"date"`
	Category string            `json:"category"`
	Tags     []string          `json:"tags"`
	Highlights map[string][]string `json:"highlights,omitempty"`
}

type AggBucket struct {
	Key   string `json:"key"`
	Count int64  `json:"count"`
}

func (c *Client) SearchPosts(ctx context.Context, q string, category, tag string, page, size int64) (*SearchResult, error) {
	must := []any{map[string]any{"term": map[string]any{"status": "published"}}}

	if q != "" {
		must = append(must, map[string]any{
			"multi_match": map[string]any{
				"query":  q,
				"fields": []string{"title^3", "content^2", "summary"},
			},
		})
	}
	if category != "" {
		must = append(must, map[string]any{"term": map[string]any{"category": category}})
	}
	if tag != "" {
		must = append(must, map[string]any{"term": map[string]any{"tags": tag}})
	}

	query := map[string]any{
		"from": (page - 1) * size,
		"size": size,
		"query": map[string]any{
			"bool": map[string]any{"must": must},
		},
		"highlight": map[string]any{
			"fields": map[string]any{
				"title":   map[string]any{"number_of_fragments": 0},
				"content": map[string]any{"fragment_size": 150, "number_of_fragments": 3},
			},
			"pre_tags":  []string{"<em>"},
			"post_tags": []string{"</em>"},
		},
		"aggs": map[string]any{
			"categories": map[string]any{"terms": map[string]any{"field": "category", "size": 20}},
			"tags":       map[string]any{"terms": map[string]any{"field": "tags", "size": 30}},
		},
	}

	body, _ := json.Marshal(query)
	res, err := c.es.Search(
		c.es.Search.WithContext(ctx),
		c.es.Search.WithIndex(indexName),
		c.es.Search.WithBody(bytes.NewReader(body)),
	)
	if err != nil {
		return nil, fmt.Errorf("es search: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		b, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("es search failed: %s", string(b))
	}

	var esRes struct {
		Hits struct {
			Total struct {
				Value int64 `json:"value"`
			} `json:"total"`
			Hits []struct {
				ID     string `json:"_id"`
				Source struct {
					Title    string   `json:"title"`
					Summary  string   `json:"summary"`
					Date     string   `json:"date"`
					Category string   `json:"category"`
					Tags     []string `json:"tags"`
				} `json:"_source"`
				Highlight map[string][]string `json:"highlight"`
			} `json:"hits"`
		} `json:"hits"`
		Aggregations map[string]struct {
			Buckets []struct {
				Key      string `json:"key"`
				DocCount int64  `json:"doc_count"`
			} `json:"buckets"`
		} `json:"aggregations"`
	}
	if err := json.NewDecoder(res.Body).Decode(&esRes); err != nil {
		return nil, fmt.Errorf("parse es response: %w", err)
	}

	sr := &SearchResult{
		Total:        esRes.Hits.Total.Value,
		Aggregations: map[string][]AggBucket{},
	}
	for _, h := range esRes.Hits.Hits {
		sr.Posts = append(sr.Posts, SearchHit{
			ID:       h.ID,
			Title:    h.Source.Title,
			Summary:  h.Source.Summary,
			Date:     h.Source.Date,
			Category: h.Source.Category,
			Tags:     h.Source.Tags,
			Highlights: h.Highlight,
		})
	}
	for name, agg := range esRes.Aggregations {
		var buckets []AggBucket
		for _, b := range agg.Buckets {
			if b.Key == "" {
				continue
			}
			buckets = append(buckets, AggBucket{Key: b.Key, Count: b.DocCount})
		}
		sr.Aggregations[name] = buckets
	}
	return sr, nil
}

func (c *Client) IsReady(ctx context.Context) bool {
	_, err := c.es.Info(c.es.Info.WithContext(ctx))
	return err == nil
}

func postDoc(p *model.Post) map[string]any {
	var tags []string
	tags = append(tags, p.Tags...)
	if tags == nil {
		tags = []string{}
	}
	return map[string]any{
		"title":    p.Title,
		"content":  p.Content,
		"summary":  p.Summary,
		"category": p.Category,
		"tags":     tags,
		"date":     p.Date,
		"status":   p.Status,
	}
}
