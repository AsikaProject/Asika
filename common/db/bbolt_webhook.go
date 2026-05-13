package db

import (
	"encoding/json"
	"fmt"
	"time"

	"asika/common/models"
)

func (s *bboltStorage) PutWebhookRetry(retry *models.WebhookRetry) error {
	data, err := json.Marshal(retry)
	if err != nil {
		return err
	}
	return s.Put(BucketWebhookRetries, retry.ID, data)
}

func (s *bboltStorage) GetWebhookRetry(id string) (*models.WebhookRetry, error) {
	data, err := s.Get(BucketWebhookRetries, id)
	if err != nil || data == nil {
		return nil, err
	}
	var retry models.WebhookRetry
	if err := json.Unmarshal(data, &retry); err != nil {
		return nil, err
	}
	return &retry, nil
}

func (s *bboltStorage) DeleteWebhookRetry(id string) error {
	return s.Delete(BucketWebhookRetries, id)
}

func (s *bboltStorage) GetDueWebhookRetries(now time.Time) ([]*models.WebhookRetry, error) {
	var due []*models.WebhookRetry
	err := s.ForEach(BucketWebhookRetries, func(key, value []byte) error {
		var retry models.WebhookRetry
		if err := json.Unmarshal(value, &retry); err != nil {
			return nil
		}
		if retry.NextRetry.IsZero() || retry.NextRetry.After(now) {
			return nil
		}
		due = append(due, &retry)
		return nil
	})
	return due, err
}

func (s *bboltStorage) PutWebhookHealth(repoGroup, platform string, ts time.Time) error {
	key := fmt.Sprintf("%s:%s", repoGroup, platform)
	return s.Put(BucketWebhookHealth, key, []byte(ts.Format(time.RFC3339)))
}

func (s *bboltStorage) GetWebhookHealth(repoGroup, platform string) (time.Time, error) {
	key := fmt.Sprintf("%s:%s", repoGroup, platform)
	data, err := s.Get(BucketWebhookHealth, key)
	if err != nil || data == nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339, string(data))
}

func (s *bboltStorage) PutWebhookDedup(deliveryID string, ts []byte) error {
	return s.Put(BucketWebhookDedup, deliveryID, ts)
}

func (s *bboltStorage) GetWebhookDedup(deliveryID string) ([]byte, error) {
	return s.Get(BucketWebhookDedup, deliveryID)
}

func (s *bboltStorage) ListWebhookHealth() (map[string]time.Time, error) {
	result := make(map[string]time.Time)
	err := s.ForEach(BucketWebhookHealth, func(key, value []byte) error {
		ts, err := time.Parse(time.RFC3339, string(value))
		if err != nil {
			return nil
		}
		result[string(key)] = ts
		return nil
	})
	return result, err
}
