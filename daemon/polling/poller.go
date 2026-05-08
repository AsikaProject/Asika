package polling

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.etcd.io/bbolt"

	"asika/common/db"
	"asika/common/events"
	"asika/common/models"
	"asika/common/platforms"
	"asika/common/utils"
)

// Poller polls platforms for PR changes
type Poller struct {
	cfg     *models.Config
	clients map[platforms.PlatformType]platforms.PlatformClient
	stop    chan struct{}
}

// NewPoller creates a new poller
func NewPoller(cfg *models.Config, clients map[platforms.PlatformType]platforms.PlatformClient) *Poller {
	return &Poller{
		cfg:     cfg,
		clients: clients,
		stop:    make(chan struct{}),
	}
}

// Start starts the polling loop
func (p *Poller) Start() {
	interval := utils.ParseDuration(p.cfg.Events.PollingInterval, 30*time.Second)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	slog.Info("polling started", "interval", interval)

	for {
		select {
		case <-ticker.C:
			p.pollOnce()
		case <-p.stop:
			slog.Info("polling stopped")
			return
		}
	}
}

// PollOnce performs a single poll cycle (can be called externally for initial fetch)
func (p *Poller) PollOnce() {
	p.pollOnce()
}

// Stop stops the poller
func (p *Poller) Stop() {
	close(p.stop)
}

func (p *Poller) pollOnce() {
	var success, failed int
	for _, repoGroup := range p.cfg.RepoGroups {
		s, f := p.pollRepoGroup(repoGroup)
		success += s
		failed += f
	}
	if total := success + failed; total > 0 {
		slog.Info("PR fetch complete", "total", total, "success", success, "failed", failed)
	}
}

func (p *Poller) pollRepoGroup(rg models.RepoGroupConfig) (success, failed int) {
	mode := rg.Mode
	if mode == "" {
		mode = "multi"
	}

	// In single mode, only poll the MirrorPlatform
	if mode == "single" && rg.MirrorPlatform != "" {
		plat := rg.MirrorPlatform
		var repo string
		switch plat {
		case "github":
			repo = rg.GitHub
		case "gitlab":
			repo = rg.GitLab
		case "gitea":
			repo = rg.Gitea
		}
		if repo == "" {
			return
		}
		client, ok := p.clients[platforms.PlatformType(plat)]
		if !ok {
			return
		}
		s, f := p.pollPlatform(client, rg.Name, plat, repo)
		return s, f
	}

	// Multi mode: poll all configured platforms
	platforms := []struct {
		ptype platforms.PlatformType
		repo  string
	}{
		{platforms.PlatformGitHub, rg.GitHub},
		{platforms.PlatformGitLab, rg.GitLab},
		{platforms.PlatformGitea, rg.Gitea},
	}

	for _, pinfo := range platforms {
		if pinfo.repo == "" {
			continue
		}
		client, ok := p.clients[pinfo.ptype]
		if !ok {
			continue
		}

		s, f := p.pollPlatform(client, rg.Name, string(pinfo.ptype), pinfo.repo)
		success += s
		failed += f
	}
	return
}

func (p *Poller) pollPlatform(client platforms.PlatformClient, repoGroup, platform, repo string) (success, failed int) {
	ctx := context.Background()

	// Parse owner/repo using the same logic as config.GetOwnerRepoFromGroup
	idx := strings.LastIndex(repo, "/")
	owner := ""
	repoName := repo
	if idx >= 0 {
		owner = repo[:idx]
		repoName = repo[idx+1:]
	}

	prs, err := client.ListPRs(ctx, owner, repoName, "all")
	if err != nil {
		slog.Error("failed to list PRs", "platform", platform, "repo", repo, "error", err)
		return 0, 1
	}

	type prSync struct {
		pr   *models.PRRecord
		key  string
		data []byte
	}
	var toWrite []prSync

	for _, pr := range prs {
		pr.RepoGroup = repoGroup
		pr.Platform = platform

		if pr.State == "closed" && !pr.MergedAt.IsZero() {
			pr.State = "merged"
		}

		key := fmt.Sprintf("%s#%s#%d", repoGroup, platform, pr.PRNumber)
		data, _ := db.Get(db.BucketPRs, key)

		var existing models.PRRecord
		if data != nil {
			if err := json.Unmarshal(data, &existing); err == nil {
				// State change detection
				if existing.State != pr.State {
					switch pr.State {
					case "open":
						events.PublishPR(events.EventPROpened, repoGroup, platform, pr, nil)
					case "closed":
						events.PublishPR(events.EventPRClosed, repoGroup, platform, pr, nil)
					case "merged":
						events.PublishPR(events.EventPRMerged, repoGroup, platform, pr, nil)
					}
				}
				// Preserve local-only fields
				pr.IsApproved = existing.IsApproved
				pr.SpamFlag = existing.SpamFlag
				if !existing.CreatedAt.IsZero() && pr.CreatedAt.IsZero() {
					pr.CreatedAt = existing.CreatedAt
				}
			}
		} else {
			pr.CreatedAt = time.Now()
			pr.UpdatedAt = time.Now()
			events.PublishPR(events.EventPROpened, repoGroup, platform, pr, nil)
		}

		if pr.ID == "" {
			pr.ID = uuid.New().String()
		}
		prData, _ := json.Marshal(pr)
		toWrite = append(toWrite, prSync{pr: pr, key: key, data: prData})
	}

	if len(toWrite) > 0 {
		err := db.Update(func(tx *bbolt.Tx) error {
			b := tx.Bucket([]byte(db.BucketPRs))
			if b == nil {
				return bbolt.ErrBucketNotFound
			}
			idxByID := tx.Bucket([]byte(db.BucketPRIndexByID))
			idxByRG := tx.Bucket([]byte(db.BucketPRIndexByRG))
			for _, item := range toWrite {
				if err := b.Put([]byte(item.key), item.data); err != nil {
					return err
				}
				if idxByID != nil && item.pr.ID != "" {
					idxByID.Put([]byte(item.pr.ID), []byte(item.key))
				}
				if idxByRG != nil && item.pr.RepoGroup != "" {
					rgKey := fmt.Sprintf("%s:%d", item.pr.RepoGroup, item.pr.PRNumber)
					idxByRG.Put([]byte(rgKey), []byte(item.key))
				}
			}
			return nil
		})
		if err != nil {
			slog.Error("failed to batch save PRs", "platform", platform, "repo", repo, "error", err)
			failed += len(toWrite)
		} else {
			success += len(toWrite)
		}
	}
	return
}


