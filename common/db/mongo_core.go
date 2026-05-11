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

type mongoStorage struct {
	client *mongo.Client
	db     *mongo.Database
}

// NewMongoStorage creates a MongoDB-backed Storage implementation.
// connStr is a MongoDB connection string, e.g. "mongodb://localhost:27017".
// dbName is the database name.
func NewMongoStorage(ctx context.Context, connStr, dbName string) (Storage, error) {
	client, err := mongo.Connect(options.Client().ApplyURI(connStr))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		client.Disconnect(ctx)
		return nil, fmt.Errorf("failed to ping MongoDB: %w", err)
	}
	s := &mongoStorage{client: client, db: client.Database(dbName)}
	if err := s.ensureIndexes(ctx); err != nil {
		client.Disconnect(ctx)
		return nil, fmt.Errorf("failed to create indexes: %w", err)
	}
	return s, nil
}

func (s *mongoStorage) ensureIndexes(ctx context.Context) error {
	_, err := s.db.Collection(BucketPRs).Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "id", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("idx_pr_id"),
	})
	if err != nil {
		return err
	}
	_, err = s.db.Collection(BucketPRs).Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "repo_group", Value: 1}, {Key: "pr_number", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("idx_pr_rg_num"),
	})
	if err != nil {
		return err
	}
	_, err = s.db.Collection(BucketUsers).Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "username", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("idx_user_name"),
	})
	if err != nil {
		return err
	}
	_, err = s.db.Collection(BucketAPIKeys).Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "id", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("idx_apikey_id"),
	})
	if err != nil {
		return err
	}
	_, err = s.db.Collection(BucketWebhookRetries).Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "next_retry", Value: 1}},
		Options: options.Index().SetName("idx_retry_next"),
	})
	return err
}

func (s *mongoStorage) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return s.client.Disconnect(ctx)
}

func (s *mongoStorage) Ping() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.client.Ping(ctx, nil)
}

func (s *mongoStorage) coll(bucket string) *mongo.Collection {
	return s.db.Collection(bucket)
}

func (s *mongoStorage) Put(bucket, key string, value []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var doc bson.D
	if err := bson.UnmarshalExtJSON(value, true, &doc); err != nil {
		doc = bson.D{{Key: "_id", Value: key}, {Key: "data", Value: string(value)}}
	}
	doc = ensureID(doc, key)
	_, err := s.coll(bucket).ReplaceOne(ctx, bson.M{"_id": key}, doc, options.Replace().SetUpsert(true))
	return err
}

func (s *mongoStorage) Get(bucket, key string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var result bson.M
	err := s.coll(bucket).FindOne(ctx, bson.M{"_id": key}).Decode(&result)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	data, err := bson.MarshalExtJSON(result, true, true)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (s *mongoStorage) Delete(bucket, key string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := s.coll(bucket).DeleteOne(ctx, bson.M{"_id": key})
	return err
}

func (s *mongoStorage) ForEach(bucket string, fn func(key, value []byte) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cursor, err := s.coll(bucket).Find(ctx, bson.M{})
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			continue
		}
		key, _ := doc["_id"].(string)
		val, err := bson.MarshalExtJSON(doc, true, true)
		if err != nil {
			continue
		}
		if err := fn([]byte(key), val); err != nil {
			return err
		}
	}
	return cursor.Err()
}

func (s *mongoStorage) ForEachPrefix(indexBucket, targetBucket, prefix string, fn func(key, value []byte) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cursor, err := s.coll(indexBucket).Find(ctx, bson.M{"_id": bson.M{"$regex": "^" + prefix}})
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var idxDoc bson.M
		if err := cursor.Decode(&idxDoc); err != nil {
			continue
		}
		key, _ := idxDoc["_id"].(string)
		targetKey, _ := idxDoc["target"].(string)
		if targetKey == "" {
			continue
		}
		var targetDoc bson.M
		if err := s.coll(targetBucket).FindOne(ctx, bson.M{"_id": targetKey}).Decode(&targetDoc); err != nil {
			continue
		}
		val, err := bson.MarshalExtJSON(targetDoc, true, true)
		if err != nil {
			continue
		}
		if err := fn([]byte(key), val); err != nil {
			return err
		}
	}
	return cursor.Err()
}

func (s *mongoStorage) BucketForEachPrefix(bucket, prefix string, fn func(key, value []byte) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cursor, err := s.coll(bucket).Find(ctx, bson.M{"_id": bson.M{"$regex": "^" + prefix}})
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			continue
		}
		key, _ := doc["_id"].(string)
		val, err := bson.MarshalExtJSON(doc, true, true)
		if err != nil {
			continue
		}
		if err := fn([]byte(key), val); err != nil {
			return err
		}
	}
	return cursor.Err()
}

func (s *mongoStorage) PutPRWithIndex(key string, value []byte, prID, repoGroup string, prNumber int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var doc bson.D
	if err := bson.UnmarshalExtJSON(value, true, &doc); err != nil {
		doc = bson.D{{Key: "_id", Value: key}, {Key: "data", Value: string(value)}}
	}
	doc = ensureID(doc, key)
	_, err := s.coll(BucketPRs).ReplaceOne(ctx, bson.M{"_id": key}, doc, options.Replace().SetUpsert(true))
	if err != nil {
		return err
	}
	if prID != "" {
		_, err = s.coll(BucketPRIndexByID).ReplaceOne(ctx, bson.M{"_id": prID}, bson.M{"_id": prID, "target": key}, options.Replace().SetUpsert(true))
		if err != nil {
			return err
		}
	}
	if repoGroup != "" {
		rgKey := fmt.Sprintf("%s:%d", repoGroup, prNumber)
		_, err = s.coll(BucketPRIndexByRG).ReplaceOne(ctx, bson.M{"_id": rgKey}, bson.M{"_id": rgKey, "target": key}, options.Replace().SetUpsert(true))
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *mongoStorage) GetPRByIndex(prID, repoGroup string, prNumber int) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var targetKey string
	if prID != "" {
		var idxDoc bson.M
		err := s.coll(BucketPRIndexByID).FindOne(ctx, bson.M{"_id": prID}).Decode(&idxDoc)
		if err == nil {
			targetKey, _ = idxDoc["target"].(string)
		}
	}
	if targetKey == "" && repoGroup != "" && prNumber > 0 {
		rgKey := fmt.Sprintf("%s:%d", repoGroup, prNumber)
		var idxDoc bson.M
		err := s.coll(BucketPRIndexByRG).FindOne(ctx, bson.M{"_id": rgKey}).Decode(&idxDoc)
		if err == nil {
			targetKey, _ = idxDoc["target"].(string)
		}
	}
	if targetKey == "" {
		return nil, nil
	}
	var doc bson.M
	err := s.coll(BucketPRs).FindOne(ctx, bson.M{"_id": targetKey}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return bson.MarshalExtJSON(doc, true, true)
}

func (s *mongoStorage) AppendAuditLogEx(entry models.AuditLog) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	data, err := bson.Marshal(entry)
	if err != nil {
		return err
	}
	var doc bson.M
	if err := bson.Unmarshal(data, &doc); err != nil {
		return err
	}
	doc["_id"] = bson.NewObjectID()
	if entry.Timestamp.IsZero() {
		doc["timestamp"] = time.Now()
	}
	_, err = s.coll(BucketLogs).InsertOne(ctx, doc)
	return err
}

func (s *mongoStorage) AppendAuditLog(level, message string, ctxMap map[string]interface{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	doc := bson.M{
		"_id":       bson.NewObjectID(),
		"timestamp": time.Now(),
		"level":     level,
		"message":   message,
		"context":   ctxMap,
	}
	_, err := s.coll(BucketLogs).InsertOne(ctx, doc)
	return err
}
