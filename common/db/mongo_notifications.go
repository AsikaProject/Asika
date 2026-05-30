package db

import (
	"context"
	"encoding/json"
	"fmt"
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

func (s *mongoStorage) AppendNotificationDigest(username, notifier, title, body string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	doc := bson.M{
		"_id":       fmt.Sprintf("%s:%s:%d", username, notifier, time.Now().UnixNano()),
		"username":  username,
		"notifier":  notifier,
		"title":     title,
		"body":      body,
		"timestamp": time.Now(),
	}
	_, err := s.coll(BucketNotificationDigest).InsertOne(ctx, doc)
	return err
}

func (s *mongoStorage) ListNotificationDigests() (map[string][]DigestEntry, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cursor, err := s.coll(BucketNotificationDigest).Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	result := make(map[string][]DigestEntry)
	for cursor.Next(ctx) {
		var entry DigestEntry
		if err := cursor.Decode(&entry); err != nil {
			continue
		}
		result[entry.Username] = append(result[entry.Username], entry)
	}
	return result, cursor.Err()
}

func (s *mongoStorage) DeleteNotificationDigests(username string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := s.coll(BucketNotificationDigest).DeleteMany(ctx, bson.M{"username": username})
	return err
}

func (s *mongoStorage) ListNotificationPrefs(usernames []string) ([]models.NotificationPreferences, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	filter := bson.M{}
	if len(usernames) > 0 {
		filter["_id"] = bson.M{"$in": usernames}
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
	return prefs, cursor.Err()
}

func (s *mongoStorage) PutPendingPR(pr *PendingPR) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	doc := bson.M{
		"_id":            fmt.Sprintf("%s#%s#%d", pr.RepoGroup, pr.Platform, pr.PRNumber),
		"pr_id":          pr.PRID,
		"repo_group":     pr.RepoGroup,
		"platform":       pr.Platform,
		"pr_number":      pr.PRNumber,
		"title":          pr.Title,
		"author":         pr.Author,
		"approval_count": pr.ApprovalCount,
		"added_at":       pr.AddedAt,
		"last_checked":   pr.LastChecked,
	}
	_, err := s.coll(BucketPendingPRs).ReplaceOne(ctx, bson.M{"_id": doc["_id"]}, doc, options.Replace().SetUpsert(true))
	return err
}

func (s *mongoStorage) GetPendingPR(repoGroup, platform string, prNumber int) (*PendingPR, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	key := fmt.Sprintf("%s#%s#%d", repoGroup, platform, prNumber)
	var doc bson.M
	err := s.coll(BucketPendingPRs).FindOne(ctx, bson.M{"_id": key}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &PendingPR{
		PRID:          doc["pr_id"].(string),
		RepoGroup:     doc["repo_group"].(string),
		Platform:      doc["platform"].(string),
		PRNumber:      int(doc["pr_number"].(int32)),
		Title:         doc["title"].(string),
		Author:        doc["author"].(string),
		ApprovalCount: int(doc["approval_count"].(int32)),
	}, nil
}

func (s *mongoStorage) DeletePendingPR(repoGroup, platform string, prNumber int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	key := fmt.Sprintf("%s#%s#%d", repoGroup, platform, prNumber)
	_, err := s.coll(BucketPendingPRs).DeleteOne(ctx, bson.M{"_id": key})
	return err
}

func (s *mongoStorage) ListPendingPRs() ([]*PendingPR, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cursor, err := s.coll(BucketPendingPRs).Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var prs []*PendingPR
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			continue
		}
		prs = append(prs, &PendingPR{
			PRID:          doc["pr_id"].(string),
			RepoGroup:     doc["repo_group"].(string),
			Platform:      doc["platform"].(string),
			PRNumber:      int(doc["pr_number"].(int32)),
			Title:         doc["title"].(string),
			Author:        doc["author"].(string),
			ApprovalCount: int(doc["approval_count"].(int32)),
		})
	}
	return prs, cursor.Err()
}
