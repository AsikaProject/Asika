package db

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
)

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
