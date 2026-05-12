package db

import (
	"context"
	"encoding/json"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"asika/common/models"
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

func (s *mongoStorage) ListNotificationPrefs(usernames []string) ([]models.NotificationPreferences, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	filter := bson.M{}
	if len(usernames) > 0 {
		filter["username"] = bson.M{"$in": usernames}
	}
	cursor, err := s.coll(BucketNotificationPrefs).Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var prefs []models.NotificationPreferences
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			continue
		}
		data, _ := doc["data"].(string)
		var p models.NotificationPreferences
		if err := json.Unmarshal([]byte(data), &p); err != nil {
			continue
		}
		prefs = append(prefs, p)
	}
	return prefs, nil
}
