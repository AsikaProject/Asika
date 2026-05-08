package telegram

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"asika/common/platforms"
)

func (b *Bot) fetchPRsForGroup(repoGroup string) []models.PRRecord {
	var prs []models.PRRecord
	db.ForEach(db.BucketPRs, func(key, value []byte) error {
		var pr models.PRRecord
		if err := json.Unmarshal(value, &pr); err != nil {
			return nil
		}
		if pr.RepoGroup == repoGroup || repoGroup == "" {
			prs = append(prs, pr)
		}
		return nil
	})
	sort.Slice(prs, func(i, j int) bool {
		return prs[i].PRNumber > prs[j].PRNumber
	})
	return prs
}

func (b *Bot) fetchOpenPRs(group *models.RepoGroup) ([]*models.PRRecord, error) {
	var prs []*models.PRRecord
	for _, pt := range platforms.GroupPlatforms(group) {
		client, ok := b.clients[pt]
		if !ok {
			continue
		}
		owner, repo := config.GetOwnerRepoFromGroup(group, string(pt))
		if owner == "" || repo == "" {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		list, err := client.ListPRs(ctx, owner, repo, "open")
		cancel()
		if err != nil {
			return nil, err
		}
		prs = append(prs, list...)
	}
	return prs, nil
}
