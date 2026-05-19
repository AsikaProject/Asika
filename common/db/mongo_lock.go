package db

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func (s *mongoStorage) AcquireSyncLock(repoGroup, holderID string, ttl time.Duration) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	key := "lock:" + repoGroup
	now := time.Now()
	expiresAt := now.Add(ttl)

	filter := bson.M{
		"_id": key,
		"$or": []bson.M{
			{"holder": holderID},
			{"expires_at": bson.M{"$lt": now}},
		},
	}
	update := bson.M{
		"$set": bson.M{
			"holder":     holderID,
			"locked_at":  now,
			"expires_at": expiresAt,
		},
	}
	opts := options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)

	var result bson.M
	err := s.coll(BucketSyncLocks).FindOneAndUpdate(ctx, filter, update, opts).Decode(&result)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return false, nil
		}
		return false, err
	}
	holder, _ := result["holder"].(string)
	return holder == holderID, nil
}

func (s *mongoStorage) ReleaseSyncLock(repoGroup, holderID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	key := "lock:" + repoGroup
	_, err := s.coll(BucketSyncLocks).DeleteOne(ctx, bson.M{"_id": key, "holder": holderID})
	return err
}

func (s *mongoStorage) isSyncLocked(repoGroup string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	key := "lock:" + repoGroup
	var result bson.M
	err := s.coll(BucketSyncLocks).FindOne(ctx, bson.M{"_id": key}).Decode(&result)
	if err == mongo.ErrNoDocuments {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	expiresAt, ok := result["expires_at"].(time.Time)
	if !ok {
		return true, nil
	}
	return time.Time(expiresAt).After(time.Now()), nil
}
