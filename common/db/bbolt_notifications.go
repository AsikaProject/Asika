package db

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
