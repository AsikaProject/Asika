package db

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"asika/common/models"
)

func (s *bboltStorage) PutTeamSpace(space *models.TeamSpace) error {
	data, err := json.Marshal(space)
	if err != nil {
		return err
	}
	return s.Put(BucketTeamSpaces, space.Name, data)
}

func (s *bboltStorage) GetTeamSpace(name string) (*models.TeamSpace, error) {
	data, err := s.Get(BucketTeamSpaces, name)
	if err != nil || data == nil {
		return nil, err
	}
	var space models.TeamSpace
	if err := json.Unmarshal(data, &space); err != nil {
		return nil, err
	}
	return &space, nil
}

func (s *bboltStorage) ListTeamSpaces() ([]*models.TeamSpace, error) {
	var spaces []*models.TeamSpace
	err := s.ForEach(BucketTeamSpaces, func(key, value []byte) error {
		var space models.TeamSpace
		if err := json.Unmarshal(value, &space); err != nil {
			return nil
		}
		spaces = append(spaces, &space)
		return nil
	})
	return spaces, err
}

func (s *bboltStorage) DeleteTeamSpace(name string) error {
	return s.Delete(BucketTeamSpaces, name)
}

func (s *bboltStorage) PutSpaceMember(spaceName, username, role string) error {
	key := fmt.Sprintf("%s:%s", spaceName, username)
	member := models.SpaceMember{Username: username, Role: role, JoinedAt: time.Now()}
	data, err := json.Marshal(member)
	if err != nil {
		return err
	}
	return s.Put(BucketSpaceMembers, key, data)
}

func (s *bboltStorage) RemoveSpaceMember(spaceName, username string) error {
	key := fmt.Sprintf("%s:%s", spaceName, username)
	return s.Delete(BucketSpaceMembers, key)
}

func (s *bboltStorage) GetSpaceMembers(spaceName string) ([]models.SpaceMember, error) {
	var members []models.SpaceMember
	prefix := spaceName + ":"
	err := s.BucketForEachPrefix(BucketSpaceMembers, prefix, func(key, value []byte) error {
		var m models.SpaceMember
		if err := json.Unmarshal(value, &m); err != nil {
			return nil
		}
		members = append(members, m)
		return nil
	})
	return members, err
}

func (s *bboltStorage) GetUserSpaces(username string) ([]string, error) {
	var spaces []string
	err := s.ForEach(BucketSpaceMembers, func(key, value []byte) error {
		parts := strings.SplitN(string(key), ":", 2)
		if len(parts) == 2 && parts[1] == username {
			spaces = append(spaces, parts[0])
		}
		return nil
	})
	return spaces, err
}

func (s *bboltStorage) PutSpaceSetting(spaceName, key string, value []byte) error {
	fullKey := fmt.Sprintf("%s:%s", spaceName, key)
	return s.Put(BucketSpaceSettings, fullKey, value)
}

func (s *bboltStorage) GetSpaceSetting(spaceName, key string) ([]byte, error) {
	fullKey := fmt.Sprintf("%s:%s", spaceName, key)
	return s.Get(BucketSpaceSettings, fullKey)
}
