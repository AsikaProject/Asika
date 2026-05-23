package db

import (
	"encoding/json"
	"fmt"
	"time"

	"asika/common/models"
)

func (s *bboltStorage) PutSession(session *models.Session) error {
	data, err := json.Marshal(session)
	if err != nil {
		return err
	}
	if err := s.Put(BucketSessions, session.ID, data); err != nil {
		return err
	}
	idxKey := fmt.Sprintf("%s:%s", session.Username, session.ID)
	return s.Put(BucketSessionsByUser, idxKey, []byte(session.ID))
}

func (s *bboltStorage) GetSession(id string) (*models.Session, error) {
	data, err := s.Get(BucketSessions, id)
	if err != nil || data == nil {
		return nil, err
	}
	var session models.Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, err
	}
	return &session, nil
}

func (s *bboltStorage) DeleteSession(id string) error {
	session, err := s.GetSession(id)
	if err != nil {
		return err
	}
	if session != nil {
		idxKey := fmt.Sprintf("%s:%s", session.Username, id)
		s.Delete(BucketSessionsByUser, idxKey)
	}
	return s.Delete(BucketSessions, id)
}

func (s *bboltStorage) ListUserSessions(username string) ([]*models.Session, error) {
	var sessions []*models.Session
	prefix := username + ":"
	err := s.BucketForEachPrefix(BucketSessionsByUser, prefix, func(key, value []byte) error {
		sessionID := string(value)
		session, err := s.GetSession(sessionID)
		if err != nil || session == nil {
			return nil
		}
		sessions = append(sessions, session)
		return nil
	})
	return sessions, err
}

func (s *bboltStorage) ListAllSessions() ([]*models.Session, error) {
	var sessions []*models.Session
	err := s.ForEach(BucketSessions, func(key, value []byte) error {
		var session models.Session
		if err := json.Unmarshal(value, &session); err != nil {
			return nil
		}
		sessions = append(sessions, &session)
		return nil
	})
	return sessions, err
}

func (s *bboltStorage) DeleteUserSessions(username string) error {
	sessions, err := s.ListUserSessions(username)
	if err != nil {
		return err
	}
	for _, session := range sessions {
		if err := s.DeleteSession(session.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *bboltStorage) DeleteInactiveSessions(before time.Time) (int, error) {
	sessions, err := s.ListAllSessions()
	if err != nil {
		return 0, err
	}
	count := 0
	for _, session := range sessions {
		if session.LastUsedAt.Before(before) {
			if err := s.DeleteSession(session.ID); err != nil {
				return count, err
			}
			count++
		}
	}
	return count, nil
}

func (s *bboltStorage) UpdateSessionActivity(id string) error {
	session, err := s.GetSession(id)
	if err != nil {
		return err
	}
	if session == nil {
		return ErrNotFound
	}
	session.LastUsedAt = time.Now()
	data, err := json.Marshal(session)
	if err != nil {
		return err
	}
	return s.Put(BucketSessions, id, data)
}

func (s *bboltStorage) PutOIDCLink(link *models.OIDCLink) error {
	data, err := json.Marshal(link)
	if err != nil {
		return err
	}
	key := fmt.Sprintf("%s:%s", link.Provider, link.Subject)
	return s.Put(BucketOIDCLinks, key, data)
}

func (s *bboltStorage) GetOIDCLink(provider, subject string) (*models.OIDCLink, error) {
	key := fmt.Sprintf("%s:%s", provider, subject)
	data, err := s.Get(BucketOIDCLinks, key)
	if err != nil || data == nil {
		return nil, err
	}
	var link models.OIDCLink
	if err := json.Unmarshal(data, &link); err != nil {
		return nil, err
	}
	return &link, nil
}

func (s *bboltStorage) DeleteOIDCLink(provider, subject string) error {
	key := fmt.Sprintf("%s:%s", provider, subject)
	return s.Delete(BucketOIDCLinks, key)
}

func (s *bboltStorage) ListOIDCLinks(username string) ([]*models.OIDCLink, error) {
	var links []*models.OIDCLink
	err := s.ForEach(BucketOIDCLinks, func(key, value []byte) error {
		var link models.OIDCLink
		if err := json.Unmarshal(value, &link); err != nil {
			return nil
		}
		if link.Username == username {
			links = append(links, &link)
		}
		return nil
	})
	return links, err
}
