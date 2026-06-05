package main

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type Store struct {
	client *mongo.Client
	db     *mongo.Database
}

func newStore(ctx context.Context, uri, dbName string) (*Store, error) {
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		return nil, fmt.Errorf("mongo connect: %w", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		return nil, fmt.Errorf("mongo ping: %w", err)
	}
	db := client.Database(dbName)
	return &Store{client: client, db: db}, nil
}

func (s *Store) postsCol() *mongo.Collection  { return s.db.Collection("posts") }
func (s *Store) adminCol() *mongo.Collection  { return s.db.Collection("admin") }
func (s *Store) settingsCol() *mongo.Collection { return s.db.Collection("settings") }

func (s *Store) initIndexes(ctx context.Context) error {
	postsCol := s.postsCol()
	indexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "date", Value: -1}}},
		{Keys: bson.D{{Key: "tags", Value: 1}}},
		{Keys: bson.D{{Key: "category", Value: 1}}},
		{Keys: bson.D{{Key: "status", Value: 1}}},
		{Keys: bson.D{
			{Key: "title", Value: "text"},
			{Key: "content", Value: "text"},
		}},
	}
	_, err := postsCol.Indexes().CreateMany(ctx, indexes)
	return err
}

// Post queries

type PostFilter struct {
	Status   string
	Tag      string
	Category string
	Search   string
}

type PostListResult struct {
	Posts []Post `json:"posts"`
	Total int64  `json:"total"`
	Page  int64  `json:"page"`
	Size  int64  `json:"size"`
}

func (s *Store) countPosts(ctx context.Context, f PostFilter) (int64, error) {
	return s.postsCol().CountDocuments(ctx, s.buildFilter(f))
}

func (s *Store) findPosts(ctx context.Context, f PostFilter, page, size int64) (*PostListResult, error) {
	filter := s.buildFilter(f)
	total, err := s.postsCol().CountDocuments(ctx, filter)
	if err != nil {
		return nil, err
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "date", Value: -1}}).
		SetSkip((page - 1) * size).
		SetLimit(size)

	cursor, err := s.postsCol().Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var posts []Post
	if err := cursor.All(ctx, &posts); err != nil {
		return nil, err
	}
	if posts == nil {
		posts = []Post{}
	}
	return &PostListResult{Posts: posts, Total: total, Page: page, Size: size}, nil
}

func (s *Store) buildFilter(f PostFilter) bson.M {
	m := bson.M{}
	if f.Status != "" {
		m["status"] = f.Status
	}
	if f.Tag != "" {
		m["tags"] = f.Tag
	}
	if f.Category != "" {
		m["category"] = f.Category
	}
	if f.Search != "" {
		m["$text"] = bson.M{"$search": f.Search}
	}
	return m
}

func (s *Store) findPost(ctx context.Context, id string) (*Post, error) {
	var p Post
	err := s.postsCol().FindOne(ctx, bson.M{"_id": id}).Decode(&p)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) insertPost(ctx context.Context, p *Post) error {
	_, err := s.postsCol().InsertOne(ctx, p)
	return err
}

func (s *Store) replacePost(ctx context.Context, p *Post) error {
	_, err := s.postsCol().ReplaceOne(ctx, bson.M{"_id": p.ID}, p)
	return err
}

func (s *Store) deletePost(ctx context.Context, id string) error {
	_, err := s.postsCol().DeleteOne(ctx, bson.M{"_id": id})
	return err
}

func (s *Store) distinctTags(ctx context.Context) ([]string, error) {
	var tags []string
	if err := s.postsCol().Distinct(ctx, "tags", bson.M{"status": "published"}).Decode(&tags); err != nil {
		return nil, err
	}
	filtered := make([]string, 0, len(tags))
	for _, t := range tags {
		if t != "" {
			filtered = append(filtered, t)
		}
	}
	return filtered, nil
}

func (s *Store) postExists(ctx context.Context, id string) bool {
	count, _ := s.postsCol().CountDocuments(ctx, bson.M{"_id": id})
	return count > 0
}

// Admin

func (s *Store) upsertAdmin(ctx context.Context, username, passwordHash string) error {
	filter := bson.M{"_id": username}
	update := bson.M{"$set": bson.M{"password_hash": passwordHash}}
	opts := options.UpdateOne().SetUpsert(true)
	_, err := s.adminCol().UpdateOne(ctx, filter, update, opts)
	return err
}

func (s *Store) findAdmin(ctx context.Context, username string) (*AdminUser, error) {
	var u AdminUser
	err := s.adminCol().FindOne(ctx, bson.M{"_id": username}).Decode(&u)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// Settings

func (s *Store) getSettings(ctx context.Context) (*BlogSettings, error) {
	var bs BlogSettings
	err := s.settingsCol().FindOne(ctx, bson.M{}).Decode(&bs)
	if err == mongo.ErrNoDocuments {
		return &BlogSettings{}, nil
	}
	if err != nil {
		return nil, err
	}
	return &bs, nil
}

func (s *Store) upsertSettings(ctx context.Context, bs *BlogSettings) error {
	filter := bson.M{}
	update := bson.M{"$set": bs}
	opts := options.UpdateOne().SetUpsert(true)
	_, err := s.settingsCol().UpdateOne(ctx, filter, update, opts)
	return err
}

func (s *Store) disconnect(ctx context.Context) error {
	return s.client.Disconnect(ctx)
}

// Helpers

func ptrTime(t time.Time) *time.Time { return &t }
