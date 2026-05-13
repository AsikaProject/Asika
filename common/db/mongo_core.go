package db

import (
	"context"
	"fmt"
	"log/slog"
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
	indexes := []struct {
		coll  string
		model mongo.IndexModel
		name  string
	}{
		{BucketPRs, mongo.IndexModel{
			Keys:    bson.D{{Key: "id", Value: 1}},
			Options: options.Index().SetUnique(true),
		}, "idx_pr_id"},
		{BucketPRs, mongo.IndexModel{
			Keys:    bson.D{{Key: "repo_group", Value: 1}, {Key: "pr_number", Value: 1}},
			Options: options.Index().SetUnique(true),
		}, "idx_pr_rg_num"},
		{BucketUsers, mongo.IndexModel{
			Keys:    bson.D{{Key: "username", Value: 1}},
			Options: options.Index().SetUnique(true),
		}, "idx_user_name"},
		{BucketAPIKeys, mongo.IndexModel{
			Keys:    bson.D{{Key: "id", Value: 1}},
			Options: options.Index().SetUnique(true),
		}, "idx_apikey_id"},
		{BucketWebhookRetries, mongo.IndexModel{
			Keys: bson.D{{Key: "next_retry", Value: 1}},
		}, "idx_retry_next"},
		{BucketIssuePRLinks, mongo.IndexModel{
			Keys: bson.D{{Key: "pr_id", Value: 1}},
		}, "idx_ipl_pr_id"},
		{BucketPRDependencies, mongo.IndexModel{
			Keys: bson.D{{Key: "depends_on_pr_id", Value: 1}},
		}, "idx_pr_dep_on"},
	}
	for _, idx := range indexes {
		if _, err := s.db.Collection(idx.coll).Indexes().CreateOne(ctx, idx.model); err != nil {
			slog.Warn("failed to create index", "collection", idx.coll, "name", idx.name, "error", err)
		}
	}
	return nil
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

	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}

	data, err := bson.Marshal(entry)
	if err != nil {
		return err
	}
	var doc bson.M
	if err := bson.Unmarshal(data, &doc); err != nil {
		return err
	}
	doc["_id"] = bson.NewObjectID()

	_, err = s.coll(BucketLogs).InsertOne(ctx, doc)
	if err != nil {
		return err
	}

	logKey := doc["_id"].(bson.ObjectID).Hex()

	if entry.Actor != "" {
		idxKey := fmt.Sprintf("actor:%s:%s", entry.Actor, logKey)
		if _, err := s.coll(BucketAuditLogIndex).ReplaceOne(ctx, bson.M{"_id": idxKey}, bson.M{"_id": idxKey, "target": logKey}, options.Replace().SetUpsert(true)); err != nil {
			slog.Error("failed to write audit log index", "key", idxKey, "error", err)
		}
	}
	if entry.RepoGroup != "" {
		idxKey := fmt.Sprintf("repo_group:%s:%s", entry.RepoGroup, logKey)
		if _, err := s.coll(BucketAuditLogIndex).ReplaceOne(ctx, bson.M{"_id": idxKey}, bson.M{"_id": idxKey, "target": logKey}, options.Replace().SetUpsert(true)); err != nil {
			slog.Error("failed to write audit log index", "key", idxKey, "error", err)
		}
	}
	if entry.Action != "" {
		idxKey := fmt.Sprintf("action:%s:%s", entry.Action, logKey)
		if _, err := s.coll(BucketAuditLogIndex).ReplaceOne(ctx, bson.M{"_id": idxKey}, bson.M{"_id": idxKey, "target": logKey}, options.Replace().SetUpsert(true)); err != nil {
			slog.Error("failed to write audit log index", "key", idxKey, "error", err)
		}
	}
	if entry.Category != "" {
		idxKey := fmt.Sprintf("category:%s:%s", entry.Category, logKey)
		if _, err := s.coll(BucketAuditLogIndex).ReplaceOne(ctx, bson.M{"_id": idxKey}, bson.M{"_id": idxKey, "target": logKey}, options.Replace().SetUpsert(true)); err != nil {
			slog.Error("failed to write audit log index", "key", idxKey, "error", err)
		}
	}
	if entry.RepoGroup != "" && entry.PRNumber > 0 {
		idxKey := fmt.Sprintf("pr:%s:%d:%s", entry.RepoGroup, entry.PRNumber, logKey)
		if _, err := s.coll(BucketAuditLogIndex).ReplaceOne(ctx, bson.M{"_id": idxKey}, bson.M{"_id": idxKey, "target": logKey}, options.Replace().SetUpsert(true)); err != nil {
			slog.Error("failed to write audit log index", "key", idxKey, "error", err)
		}
	}
	return nil
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

func (s *mongoStorage) PutIssuePRLink(link *models.IssuePRLink) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	doc := bson.M{
		"_id":        fmt.Sprintf("%s:%s", link.IssueID, link.PRID),
		"issue_id":   link.IssueID,
		"pr_id":      link.PRID,
		"repo_group": link.RepoGroup,
		"platform":   link.Platform,
		"link_type":  link.LinkType,
	}
	id := doc["_id"]
	if _, err := s.coll(BucketIssuePRLinks).InsertOne(ctx, doc); err != nil {
		if _, err := s.coll(BucketIssuePRLinks).ReplaceOne(ctx, bson.M{"_id": id}, doc, options.Replace().SetUpsert(true)); err != nil {
			return err
		}
	}
	revDoc := bson.M{
		"_id":  fmt.Sprintf("%s:%s", link.PRID, link.IssueID),
		"link": id,
	}
	if _, err := s.coll(BucketIssuePRLinksByPR).InsertOne(ctx, revDoc); err != nil {
		s.coll(BucketIssuePRLinksByPR).ReplaceOne(ctx, bson.M{"_id": revDoc["_id"]}, revDoc, options.Replace().SetUpsert(true))
	}
	return nil
}

func (s *mongoStorage) GetIssuePRLinksByIssue(issueID string) ([]*models.IssuePRLink, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cursor, err := s.coll(BucketIssuePRLinks).Find(ctx, bson.M{"issue_id": issueID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var links []*models.IssuePRLink
	for cursor.Next(ctx) {
		var link models.IssuePRLink
		if err := cursor.Decode(&link); err != nil {
			continue
		}
		links = append(links, &link)
	}
	return links, nil
}

func (s *mongoStorage) GetIssuePRLinksByPR(prID string) ([]*models.IssuePRLink, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cursor, err := s.coll(BucketIssuePRLinks).Find(ctx, bson.M{"pr_id": prID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var links []*models.IssuePRLink
	for cursor.Next(ctx) {
		var link models.IssuePRLink
		if err := cursor.Decode(&link); err != nil {
			continue
		}
		links = append(links, &link)
	}
	return links, nil
}

func (s *mongoStorage) PutPRDependency(dep *models.PRDependency) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	doc := bson.M{
		"_id":              fmt.Sprintf("%s:%s", dep.PRID, dep.DependsOnPRID),
		"pr_id":            dep.PRID,
		"depends_on_pr_id": dep.DependsOnPRID,
		"depends_on_url":   dep.DependsOnURL,
		"repo_group":       dep.RepoGroup,
		"platform":         dep.Platform,
	}
	_, err := s.coll(BucketPRDependencies).InsertOne(ctx, doc)
	if err != nil {
		_, err = s.coll(BucketPRDependencies).ReplaceOne(ctx, bson.M{"_id": doc["_id"]}, doc, options.Replace().SetUpsert(true))
	}
	return err
}

func (s *mongoStorage) GetPRDependenciesByPR(prID string) ([]*models.PRDependency, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cursor, err := s.coll(BucketPRDependencies).Find(ctx, bson.M{"pr_id": prID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var deps []*models.PRDependency
	for cursor.Next(ctx) {
		var dep models.PRDependency
		if err := cursor.Decode(&dep); err != nil {
			continue
		}
		deps = append(deps, &dep)
	}
	return deps, nil
}

func (s *mongoStorage) GetPRDependentsByPR(prID string) ([]*models.PRDependency, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cursor, err := s.coll(BucketPRDependencies).Find(ctx, bson.M{"depends_on_pr_id": prID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var deps []*models.PRDependency
	for cursor.Next(ctx) {
		var dep models.PRDependency
		if err := cursor.Decode(&dep); err != nil {
			continue
		}
		deps = append(deps, &dep)
	}
	return deps, nil
}

func (s *mongoStorage) PutPRTemplate(tpl *models.PRTemplate) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	doc := bson.M{
		"_id":           fmt.Sprintf("%s:%s", tpl.RepoGroup, tpl.Platform),
		"repo_group":    tpl.RepoGroup,
		"platform":      tpl.Platform,
		"content":       tpl.Content,
		"has_checklist": tpl.HasChecklist,
	}
	_, err := s.coll(BucketPRTemplates).InsertOne(ctx, doc)
	if err != nil {
		_, err = s.coll(BucketPRTemplates).ReplaceOne(ctx, bson.M{"_id": doc["_id"]}, doc, options.Replace().SetUpsert(true))
	}
	return err
}

func (s *mongoStorage) GetPRTemplate(repoGroup, platform string) (*models.PRTemplate, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	id := fmt.Sprintf("%s:%s", repoGroup, platform)
	var tpl models.PRTemplate
	err := s.coll(BucketPRTemplates).FindOne(ctx, bson.M{"_id": id}).Decode(&tpl)
	if err != nil {
		return nil, err
	}
	return &tpl, nil
}

func (s *mongoStorage) PutPRStack(stack *models.PRStack) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	doc := bson.M{
		"_id":         stack.ID,
		"name":        stack.Name,
		"description": stack.Description,
		"author":      stack.Author,
		"state":       stack.State,
		"members":     stack.Members,
		"created_at":  stack.CreatedAt,
		"updated_at":  stack.UpdatedAt,
	}
	_, err := s.coll(BucketPRStacks).InsertOne(ctx, doc)
	if err != nil {
		_, err = s.coll(BucketPRStacks).ReplaceOne(ctx, bson.M{"_id": stack.ID}, doc, options.Replace().SetUpsert(true))
	}
	return err
}

func (s *mongoStorage) GetPRStack(id string) (*models.PRStack, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var stack models.PRStack
	err := s.coll(BucketPRStacks).FindOne(ctx, bson.M{"_id": id}).Decode(&stack)
	if err != nil {
		return nil, err
	}
	return &stack, nil
}

func (s *mongoStorage) ListPRStacks() ([]*models.PRStack, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cursor, err := s.coll(BucketPRStacks).Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var stacks []*models.PRStack
	for cursor.Next(ctx) {
		var stack models.PRStack
		if err := cursor.Decode(&stack); err != nil {
			continue
		}
		stacks = append(stacks, &stack)
	}
	return stacks, nil
}

func (s *mongoStorage) DeletePRStack(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := s.coll(BucketPRStacks).DeleteOne(ctx, bson.M{"_id": id})
	return err
}
