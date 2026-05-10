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

func (s *mongoStorage) PutConfigSnapshot(version int, data []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	key := fmt.Sprintf("%06d", version)
	doc := bson.M{"_id": key, "version": version, "data": string(data)}
	_, err := s.coll(BucketConfigHistory).ReplaceOne(ctx, bson.M{"_id": key}, doc, options.Replace().SetUpsert(true))
	return err
}

func (s *mongoStorage) GetConfigSnapshot(version int) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	key := fmt.Sprintf("%06d", version)
	var doc bson.M
	err := s.coll(BucketConfigHistory).FindOne(ctx, bson.M{"_id": key}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	data, _ := doc["data"].(string)
	return []byte(data), nil
}

func (s *mongoStorage) ListConfigSnapshots(limit int) ([]ConfigSnapshotEntry, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	opts := options.Find().SetSort(bson.D{{Key: "_id", Value: -1}})
	if limit > 0 {
		opts.SetLimit(int64(limit))
	}
	cursor, err := s.coll(BucketConfigHistory).Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var results []ConfigSnapshotEntry
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			continue
		}
		ver, _ := doc["version"].(int32)
		data, _ := doc["data"].(string)
		results = append(results, ConfigSnapshotEntry{Version: int(ver), Data: []byte(data)})
	}
	return results, cursor.Err()
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

func (s *mongoStorage) PutAPIKey(key *models.APIKey) error {
	data, err := bson.Marshal(key)
	if err != nil {
		return err
	}
	var doc bson.M
	if err := bson.Unmarshal(data, &doc); err != nil {
		return err
	}
	doc["_id"] = key.ID
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = s.coll(BucketAPIKeys).ReplaceOne(ctx, bson.M{"_id": key.ID}, doc, options.Replace().SetUpsert(true))
	return err
}

func (s *mongoStorage) GetAPIKey(id string) (*models.APIKey, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var doc bson.M
	err := s.coll(BucketAPIKeys).FindOne(ctx, bson.M{"_id": id}).Decode(&doc)
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
	var key models.APIKey
	if err := bson.Unmarshal(data, &key); err != nil {
		return nil, err
	}
	return &key, nil
}

func (s *mongoStorage) DeleteAPIKey(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := s.coll(BucketAPIKeys).DeleteOne(ctx, bson.M{"_id": id})
	return err
}

func (s *mongoStorage) ListAPIKeys() ([]*models.APIKey, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cursor, err := s.coll(BucketAPIKeys).Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var keys []*models.APIKey
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			continue
		}
		data, err := bson.Marshal(doc)
		if err != nil {
			continue
		}
		var key models.APIKey
		if err := bson.Unmarshal(data, &key); err != nil {
			continue
		}
		keys = append(keys, &key)
	}
	return keys, cursor.Err()
}

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

func (s *mongoStorage) PutTeamSpace(space *models.TeamSpace) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	data, err := bson.Marshal(space)
	if err != nil {
		return err
	}
	var doc bson.M
	if err := bson.Unmarshal(data, &doc); err != nil {
		return err
	}
	doc["_id"] = space.Name
	_, err = s.coll(BucketTeamSpaces).ReplaceOne(ctx, bson.M{"_id": space.Name}, doc, options.Replace().SetUpsert(true))
	return err
}

func (s *mongoStorage) GetTeamSpace(name string) (*models.TeamSpace, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var doc bson.M
	err := s.coll(BucketTeamSpaces).FindOne(ctx, bson.M{"_id": name}).Decode(&doc)
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
	var space models.TeamSpace
	if err := bson.Unmarshal(data, &space); err != nil {
		return nil, err
	}
	return &space, nil
}

func (s *mongoStorage) ListTeamSpaces() ([]*models.TeamSpace, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cursor, err := s.coll(BucketTeamSpaces).Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var spaces []*models.TeamSpace
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			continue
		}
		data, err := bson.Marshal(doc)
		if err != nil {
			continue
		}
		var space models.TeamSpace
		if err := bson.Unmarshal(data, &space); err != nil {
			continue
		}
		spaces = append(spaces, &space)
	}
	return spaces, cursor.Err()
}

func (s *mongoStorage) DeleteTeamSpace(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := s.coll(BucketTeamSpaces).DeleteOne(ctx, bson.M{"_id": name})
	return err
}

func (s *mongoStorage) PutSpaceMember(spaceName, username, role string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	key := fmt.Sprintf("%s:%s", spaceName, username)
	doc := bson.M{"_id": key, "space_name": spaceName, "username": username, "role": role, "joined_at": time.Now()}
	_, err := s.coll(BucketSpaceMembers).ReplaceOne(ctx, bson.M{"_id": key}, doc, options.Replace().SetUpsert(true))
	return err
}

func (s *mongoStorage) RemoveSpaceMember(spaceName, username string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	key := fmt.Sprintf("%s:%s", spaceName, username)
	_, err := s.coll(BucketSpaceMembers).DeleteOne(ctx, bson.M{"_id": key})
	return err
}

func (s *mongoStorage) GetSpaceMembers(spaceName string) ([]models.SpaceMember, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cursor, err := s.coll(BucketSpaceMembers).Find(ctx, bson.M{"space_name": spaceName})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var members []models.SpaceMember
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			continue
		}
		data, err := bson.Marshal(doc)
		if err != nil {
			continue
		}
		var m models.SpaceMember
		if err := bson.Unmarshal(data, &m); err != nil {
			continue
		}
		members = append(members, m)
	}
	return members, cursor.Err()
}

func (s *mongoStorage) GetUserSpaces(username string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cursor, err := s.coll(BucketSpaceMembers).Find(ctx, bson.M{"username": username})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var spaces []string
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			continue
		}
		sn, _ := doc["space_name"].(string)
		if sn != "" {
			spaces = append(spaces, sn)
		}
	}
	return spaces, cursor.Err()
}

func (s *mongoStorage) PutSpaceSetting(spaceName, key string, value []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	fullKey := fmt.Sprintf("%s:%s", spaceName, key)
	doc := bson.M{"_id": fullKey, "value": string(value)}
	_, err := s.coll(BucketSpaceSettings).ReplaceOne(ctx, bson.M{"_id": fullKey}, doc, options.Replace().SetUpsert(true))
	return err
}

func (s *mongoStorage) GetSpaceSetting(spaceName, key string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	fullKey := fmt.Sprintf("%s:%s", spaceName, key)
	var doc bson.M
	err := s.coll(BucketSpaceSettings).FindOne(ctx, bson.M{"_id": fullKey}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	val, _ := doc["value"].(string)
	return []byte(val), nil
}

func (s *mongoStorage) DeleteNotificationDedup(key string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := s.coll(BucketNotificationDedup).DeleteOne(ctx, bson.M{"_id": key})
	return err
}

func (s *mongoStorage) PutReportHistory(id string, data []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var doc bson.M
	if err := bson.Unmarshal(data, &doc); err != nil {
		doc = bson.M{"_id": id, "data": string(data)}
	}
	doc["_id"] = id
	_, err := s.coll(BucketReportHistory).ReplaceOne(ctx, bson.M{"_id": id}, doc, options.Replace().SetUpsert(true))
	return err
}

func (s *mongoStorage) ListReportHistory(limit int) ([]ReportHistoryEntry, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	opts := options.Find().SetSort(bson.D{{Key: "timestamp", Value: -1}})
	if limit > 0 {
		opts.SetLimit(int64(limit))
	}
	cursor, err := s.coll(BucketReportHistory).Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var results []ReportHistoryEntry
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			continue
		}
		data, err := bson.Marshal(doc)
		if err != nil {
			continue
		}
		var entry ReportHistoryEntry
		if err := bson.Unmarshal(data, &entry); err != nil {
			continue
		}
		if entry.ID == "" {
			entry.ID, _ = doc["_id"].(string)
		}
		results = append(results, entry)
	}
	return results, cursor.Err()
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

func (s *mongoStorage) PutSpamAuthor(author *models.SpamAuthor) error {
	data, err := bson.Marshal(author)
	if err != nil {
		return err
	}
	var doc bson.M
	if err := bson.Unmarshal(data, &doc); err != nil {
		return err
	}
	doc["_id"] = fmt.Sprintf("%s:%s", author.Author, author.Platform)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = s.coll(BucketSpamAuthors).ReplaceOne(ctx, bson.M{"_id": doc["_id"]}, doc, options.Replace().SetUpsert(true))
	return err
}

func (s *mongoStorage) GetSpamAuthor(author, platform string) (*models.SpamAuthor, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	id := fmt.Sprintf("%s:%s", author, platform)
	var doc bson.M
	err := s.coll(BucketSpamAuthors).FindOne(ctx, bson.M{"_id": id}).Decode(&doc)
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
	var sa models.SpamAuthor
	if err := bson.Unmarshal(data, &sa); err != nil {
		return nil, err
	}
	return &sa, nil
}

func (s *mongoStorage) ListSpamAuthors() ([]*models.SpamAuthor, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cursor, err := s.coll(BucketSpamAuthors).Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var authors []*models.SpamAuthor
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			continue
		}
		data, err := bson.Marshal(doc)
		if err != nil {
			continue
		}
		var sa models.SpamAuthor
		if err := bson.Unmarshal(data, &sa); err != nil {
			continue
		}
		authors = append(authors, &sa)
	}
	return authors, cursor.Err()
}

func ensureID(doc bson.D, key string) bson.D {
	for _, elem := range doc {
		if elem.Key == "_id" {
			return doc
		}
	}
	return append(bson.D{{Key: "_id", Value: key}}, doc...)
}

func isProtectedBucket(bucket string) bool {
	switch bucket {
	case BucketPRIndexByID, BucketPRIndexByRG:
		return true
	}
	return false
}

func MigrateBboltToMongo(ctx context.Context, bboltPath, mongoConnStr, mongoDBName string) error {
	bboltStore, err := newBboltStorage(bboltPath)
	if err != nil {
		return fmt.Errorf("failed to open bbolt: %w", err)
	}
	defer bboltStore.Close()

	mongoStore, err := NewMongoStorage(ctx, mongoConnStr, mongoDBName)
	if err != nil {
		return fmt.Errorf("failed to connect to MongoDB: %w", err)
	}
	defer mongoStore.Close()

	buckets := []string{
		BucketConfig, BucketRepos, BucketPRs, BucketLogs, BucketQueueItems,
		BucketUsers, BucketSyncHistory, BucketWebhookRetries, BucketConfigHistory,
		BucketAPIKeys, BucketPRIndexByID, BucketPRIndexByRG, BucketSpamAuthors,
		BucketWebhookHealth, BucketReportHistory, BucketNotificationPrefs,
		BucketNotificationDedup, BucketTeamSpaces, BucketSpaceMembers,
		BucketSpaceSettings,
	}

	for _, bucket := range buckets {
		if isProtectedBucket(bucket) {
			continue
		}
		err := bboltStore.ForEach(bucket, func(key, value []byte) error {
			return mongoStore.Put(bucket, string(key), value)
		})
		if err != nil {
			return fmt.Errorf("failed to migrate bucket %s: %w", bucket, err)
		}
	}

	return nil
}

func MigrateMongoToBbolt(ctx context.Context, mongoConnStr, mongoDBName, bboltPath string) error {
	mongoStore, err := NewMongoStorage(ctx, mongoConnStr, mongoDBName)
	if err != nil {
		return fmt.Errorf("failed to connect to MongoDB: %w", err)
	}
	defer mongoStore.Close()

	bboltStore, err := newBboltStorage(bboltPath)
	if err != nil {
		return fmt.Errorf("failed to open bbolt: %w", err)
	}
	defer bboltStore.Close()

	buckets := []string{
		BucketConfig, BucketRepos, BucketPRs, BucketLogs, BucketQueueItems,
		BucketUsers, BucketSyncHistory, BucketWebhookRetries, BucketConfigHistory,
		BucketAPIKeys, BucketSpamAuthors, BucketWebhookHealth, BucketReportHistory,
		BucketNotificationPrefs, BucketNotificationDedup,
	}

	for _, bucket := range buckets {
		err := mongoStore.ForEach(bucket, func(key, value []byte) error {
			return bboltStore.Put(bucket, string(key), value)
		})
		if err != nil {
			return fmt.Errorf("failed to migrate bucket %s: %w", bucket, err)
		}
	}

	return nil
}
