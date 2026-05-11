package db

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func (s *mongoStorage) PutNotificationPrefs(username string, data []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	doc := bson.M{"_id": username, "data": string(data)}
	_, err := s.coll(BucketNotificationPrefs).ReplaceOne(ctx, bson.M{"_id": username}, doc, options.Replace().SetUpsert(true))
	return err
}

func (s *mongoStorage) GetNotificationPrefs(username string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var doc bson.M
	err := s.coll(BucketNotificationPrefs).FindOne(ctx, bson.M{"_id": username}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	data, _ := doc["data"].(string)
	return []byte(data), nil
}

func (s *mongoStorage) PutNotificationDedup(key string, data []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	doc := bson.M{"_id": key, "data": string(data)}
	_, err := s.coll(BucketNotificationDedup).ReplaceOne(ctx, bson.M{"_id": key}, doc, options.Replace().SetUpsert(true))
	return err
}

func (s *mongoStorage) GetNotificationDedup(key string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var doc bson.M
	err := s.coll(BucketNotificationDedup).FindOne(ctx, bson.M{"_id": key}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	data, _ := doc["data"].(string)
	return []byte(data), nil
}

func (s *mongoStorage) DeleteNotificationDedup(key string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := s.coll(BucketNotificationDedup).DeleteOne(ctx, bson.M{"_id": key})
	return err
}
