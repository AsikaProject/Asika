package db

import (
	"encoding/json"
	"fmt"

	"asika/common/models"
)

func (s *bboltStorage) PutAPIKey(key *models.APIKey) error {
	data, err := json.Marshal(key)
	if err != nil {
		return err
	}
	return s.Put(BucketAPIKeys, key.ID, data)
}

func (s *bboltStorage) GetAPIKey(id string) (*models.APIKey, error) {
	data, err := s.Get(BucketAPIKeys, id)
	if err != nil {
		return nil, err
	}
	var key models.APIKey
	if err := json.Unmarshal(data, &key); err != nil {
		return nil, err
	}
	return &key, nil
}

func (s *bboltStorage) DeleteAPIKey(id string) error {
	return s.Delete(BucketAPIKeys, id)
}

func (s *bboltStorage) ListAPIKeys() ([]*models.APIKey, error) {
	var keys []*models.APIKey
	err := s.ForEach(BucketAPIKeys, func(key, value []byte) error {
		var k models.APIKey
		if err := json.Unmarshal(value, &k); err != nil {
			return nil
		}
		keys = append(keys, &k)
		return nil
	})
	return keys, err
}

func (s *bboltStorage) PutSpamAuthor(author *models.SpamAuthor) error {
	data, err := json.Marshal(author)
	if err != nil {
		return err
	}
	key := fmt.Sprintf("%s:%s", author.Author, author.Platform)
	return s.Put(BucketSpamAuthors, key, data)
}

func (s *bboltStorage) GetSpamAuthor(author, platform string) (*models.SpamAuthor, error) {
	key := fmt.Sprintf("%s:%s", author, platform)
	data, err := s.Get(BucketSpamAuthors, key)
	if err != nil || data == nil {
		return nil, err
	}
	var sa models.SpamAuthor
	if err := json.Unmarshal(data, &sa); err != nil {
		return nil, err
	}
	return &sa, nil
}

func (s *bboltStorage) ListSpamAuthors() ([]*models.SpamAuthor, error) {
	var authors []*models.SpamAuthor
	err := s.ForEach(BucketSpamAuthors, func(key, value []byte) error {
		var sa models.SpamAuthor
		if err := json.Unmarshal(value, &sa); err != nil {
			return nil
		}
		authors = append(authors, &sa)
		return nil
	})
	return authors, err
}
