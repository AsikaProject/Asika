package db

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

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
