package db

import (
	"encoding/json"

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
