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

func (s *mongoStorage) PutSession(session *models.Session) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	doc := bson.M{
		"_id":          session.ID,
		"username":     session.Username,
		"token_prefix": session.TokenPrefix,
		"issued_at":    session.IssuedAt,
		"last_used_at": session.LastUsedAt,
		"expires_at":   session.ExpiresAt,
		"ip_address":   session.IPAddress,
		"user_agent":   session.UserAgent,
	}
	_, err := s.coll(BucketSessions).ReplaceOne(ctx, bson.M{"_id": session.ID}, doc, options.Replace().SetUpsert(true))
	return err
}

func (s *mongoStorage) GetSession(id string) (*models.Session, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var session models.Session
	err := s.coll(BucketSessions).FindOne(ctx, bson.M{"_id": id}).Decode(&session)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return &session, nil
}

func (s *mongoStorage) DeleteSession(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := s.coll(BucketSessions).DeleteOne(ctx, bson.M{"_id": id})
	return err
}

func (s *mongoStorage) ListUserSessions(username string) ([]*models.Session, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cursor, err := s.coll(BucketSessions).Find(ctx, bson.M{"username": username})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var sessions []*models.Session
	for cursor.Next(ctx) {
		var session models.Session
		if err := cursor.Decode(&session); err != nil {
			continue
		}
		sessions = append(sessions, &session)
	}
	return sessions, cursor.Err()
}

func (s *mongoStorage) ListAllSessions() ([]*models.Session, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cursor, err := s.coll(BucketSessions).Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var sessions []*models.Session
	for cursor.Next(ctx) {
		var session models.Session
		if err := cursor.Decode(&session); err != nil {
			continue
		}
		sessions = append(sessions, &session)
	}
	return sessions, cursor.Err()
}

func (s *mongoStorage) DeleteUserSessions(username string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := s.coll(BucketSessions).DeleteMany(ctx, bson.M{"username": username})
	return err
}

func (s *mongoStorage) DeleteInactiveSessions(before time.Time) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := s.coll(BucketSessions).DeleteMany(ctx, bson.M{"last_used_at": bson.M{"$lt": before}})
	if err != nil {
		return 0, err
	}
	return int(result.DeletedCount), nil
}

func (s *mongoStorage) UpdateSessionActivity(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := s.coll(BucketSessions).UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"last_used_at": time.Now()}})
	return err
}

func (s *mongoStorage) PutOIDCLink(link *models.OIDCLink) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	doc := bson.M{
		"_id":          fmt.Sprintf("%s:%s", link.Provider, link.Subject),
		"provider":     link.Provider,
		"subject":      link.Subject,
		"username":     link.Username,
		"created_at":   link.CreatedAt,
		"last_used_at": link.LastUsedAt,
	}
	_, err := s.coll(BucketOIDCLinks).ReplaceOne(ctx, bson.M{"_id": doc["_id"]}, doc, options.Replace().SetUpsert(true))
	return err
}

func (s *mongoStorage) GetOIDCLink(provider, subject string) (*models.OIDCLink, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	id := fmt.Sprintf("%s:%s", provider, subject)
	var link models.OIDCLink
	err := s.coll(BucketOIDCLinks).FindOne(ctx, bson.M{"_id": id}).Decode(&link)
	if err != nil {
		return nil, err
	}
	return &link, nil
}

func (s *mongoStorage) DeleteOIDCLink(provider, subject string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	id := fmt.Sprintf("%s:%s", provider, subject)
	_, err := s.coll(BucketOIDCLinks).DeleteOne(ctx, bson.M{"_id": id})
	return err
}

func (s *mongoStorage) ListOIDCLinks(username string) ([]*models.OIDCLink, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cursor, err := s.coll(BucketOIDCLinks).Find(ctx, bson.M{"username": username})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var links []*models.OIDCLink
	for cursor.Next(ctx) {
		var link models.OIDCLink
		if err := cursor.Decode(&link); err != nil {
			continue
		}
		links = append(links, &link)
	}
	return links, cursor.Err()
}
