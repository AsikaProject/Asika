package db

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"asika/common/models"
)

func (s *mongoStorage) PutWebhookRetry(retry *models.WebhookRetry) error {
	data, err := bson.Marshal(retry)
	if err != nil {
		return err
	}
	var doc bson.M
	if err := bson.Unmarshal(data, &doc); err != nil {
		return err
	}
	doc["_id"] = retry.ID
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = s.coll(BucketWebhookRetries).ReplaceOne(ctx, bson.M{"_id": retry.ID}, doc, options.Replace().SetUpsert(true))
	return err
}

func (s *mongoStorage) GetWebhookRetry(id string) (*models.WebhookRetry, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var doc bson.M
	err := s.coll(BucketWebhookRetries).FindOne(ctx, bson.M{"_id": id}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	data, err := bson.Marshal(doc)
	if err != nil {
		return nil, err
	}
	var retry models.WebhookRetry
	if err := bson.Unmarshal(data, &retry); err != nil {
		return nil, err
	}
	return &retry, nil
}

func (s *mongoStorage) DeleteWebhookRetry(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := s.coll(BucketWebhookRetries).DeleteOne(ctx, bson.M{"_id": id})
	return err
}

func (s *mongoStorage) GetDueWebhookRetries(now time.Time) ([]*models.WebhookRetry, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cursor, err := s.coll(BucketWebhookRetries).Find(ctx, bson.M{"next_retry": bson.M{"$lte": now}})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var retries []*models.WebhookRetry
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			continue
		}
		data, err := bson.Marshal(doc)
		if err != nil {
			continue
		}
		var retry models.WebhookRetry
		if err := bson.Unmarshal(data, &retry); err != nil {
			continue
		}
		retries = append(retries, &retry)
	}
	return retries, cursor.Err()
}

func (s *mongoStorage) PutWebhookHealth(repoGroup, platform string, ts time.Time) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	key := fmt.Sprintf("%s:%s", repoGroup, platform)
	doc := bson.M{"_id": key, "ts": ts}
	_, err := s.coll(BucketWebhookHealth).ReplaceOne(ctx, bson.M{"_id": key}, doc, options.Replace().SetUpsert(true))
	return err
}

func (s *mongoStorage) GetWebhookHealth(repoGroup, platform string) (time.Time, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	key := fmt.Sprintf("%s:%s", repoGroup, platform)
	var doc bson.M
	err := s.coll(BucketWebhookHealth).FindOne(ctx, bson.M{"_id": key}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	ts, ok := doc["ts"].(bson.DateTime)
	if !ok {
		return time.Time{}, nil
	}
	return ts.Time(), nil
}

func (s *mongoStorage) ListWebhookHealth() (map[string]time.Time, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cursor, err := s.coll(BucketWebhookHealth).Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	result := make(map[string]time.Time)
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			continue
		}
		key, _ := doc["_id"].(string)
		ts, ok := doc["ts"].(bson.DateTime)
		if !ok {
			continue
		}
		result[key] = ts.Time()
	}
	return result, cursor.Err()
}

func (s *mongoStorage) PutWebhookDedup(deliveryID string, ts []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	doc := bson.M{"_id": deliveryID, "ts": string(ts)}
	_, err := s.coll(BucketWebhookDedup).ReplaceOne(ctx, bson.M{"_id": deliveryID}, doc, options.Replace().SetUpsert(true))
	return err
}

func (s *mongoStorage) GetWebhookDedup(deliveryID string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var doc bson.M
	err := s.coll(BucketWebhookDedup).FindOne(ctx, bson.M{"_id": deliveryID}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	ts, _ := doc["ts"].(string)
	return []byte(ts), nil
}
