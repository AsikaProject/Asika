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
