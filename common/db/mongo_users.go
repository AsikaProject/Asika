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
