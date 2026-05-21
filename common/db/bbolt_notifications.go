package db

import (
	"encoding/json"
	"fmt"
	"time"

	"asika/common/models"
)

func (s *bboltStorage) PutNotificationPrefs(username string, data []byte) error {
	return s.Put(BucketNotificationPrefs, username, data)
}

func (s *bboltStorage) GetNotificationPrefs(username string) ([]byte, error) {
	return s.Get(BucketNotificationPrefs, username)
}

func (s *bboltStorage) PutNotificationDedup(key string, data []byte) error {
	return s.Put(BucketNotificationDedup, key, data)
}

func (s *bboltStorage) GetNotificationDedup(key string) ([]byte, error) {
	return s.Get(BucketNotificationDedup, key)
}

func (s *bboltStorage) DeleteNotificationDedup(key string) error {
	return s.Delete(BucketNotificationDedup, key)
}

func (s *bboltStorage) PutNotificationDigest(key string, data []byte) error {
	return s.Put(BucketNotificationDigest, key, data)
}

func (s *bboltStorage) GetNotificationDigest(key string) ([]byte, error) {
	return s.Get(BucketNotificationDigest, key)
}

func (s *bboltStorage) DeleteNotificationDigest(key string) error {
	return s.Delete(BucketNotificationDigest, key)
}

type DigestEntry struct {
	Username  string    `json:"username"`
	Notifier  string    `json:"notifier"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Timestamp time.Time `json:"timestamp"`
}

func (s *bboltStorage) AppendNotificationDigest(username, notifier string, title, body string) error {
	key := fmt.Sprintf("%s:%s:%d", username, notifier, time.Now().UnixNano())
	entry := DigestEntry{
		Username:  username,
		Notifier:  notifier,
		Title:     title,
		Body:      body,
		Timestamp: time.Now(),
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return s.PutNotificationDigest(key, data)
}

func (s *bboltStorage) ListNotificationDigests() (map[string][]DigestEntry, error) {
	result := make(map[string][]DigestEntry)
	err := s.ForEach(BucketNotificationDigest, func(key, value []byte) error {
		var entry DigestEntry
		if err := json.Unmarshal(value, &entry); err != nil {
			return nil
		}
		result[entry.Username] = append(result[entry.Username], entry)
		return nil
	})
	return result, err
}

func (s *bboltStorage) DeleteNotificationDigests(username string) error {
	var keys []string
	err := s.ForEach(BucketNotificationDigest, func(key, value []byte) error {
		var entry DigestEntry
		if err := json.Unmarshal(value, &entry); err != nil {
			return nil
		}
		if entry.Username == username {
			keys = append(keys, string(key))
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, k := range keys {
		if delErr := s.Delete(BucketNotificationDigest, k); delErr != nil {
			return delErr
		}
	}
	return nil
}

func (s *bboltStorage) ListNotificationPrefs(usernames []string) ([]models.NotificationPreferences, error) {
	var prefs []models.NotificationPreferences
	err := s.ForEach(BucketNotificationPrefs, func(key, value []byte) error {
		var p models.NotificationPreferences
		if err := json.Unmarshal(value, &p); err != nil {
			return nil
		}
		if len(usernames) > 0 {
			for _, u := range usernames {
				if p.Username == u {
					prefs = append(prefs, p)
					return nil
				}
			}
			return nil
		}
		prefs = append(prefs, p)
		return nil
	})
	return prefs, err
}
