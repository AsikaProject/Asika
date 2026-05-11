package handlers

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"asika/common/config"
	"asika/common/models"
	"asika/daemon/feed"
)

// GetFeed handles GET /api/v1/feed.xml
func GetFeed(c *gin.Context) {
	f := feed.GlobalFeed()
	cfg := f.GetConfig()

	if !cfg.Enabled {
		c.JSON(http.StatusNotFound, gin.H{"error": "feed is disabled"})
		return
	}

	repoGroup := c.Query("repo_group")

	baseURL := fmt.Sprintf("http://%s", c.Request.Host)
	if c.Request.TLS != nil {
		baseURL = fmt.Sprintf("https://%s", c.Request.Host)
	}

	var items []feed.FeedItem
	if repoGroup != "" {
		items = f.GetItemsForRepoGroup(repoGroup)
	} else {
		items = f.GetItems()
	}

	rssXML, err := f.GenerateRSS(items, baseURL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate feed"})
		return
	}

	c.Data(http.StatusOK, "application/rss+xml; charset=utf-8", rssXML)
}

// GetFeedConfig handles GET /api/v1/feed/config
func GetFeedConfig(c *gin.Context) {
	f := feed.GlobalFeed()
	c.JSON(http.StatusOK, f.GetConfig())
}

// UpdateFeedConfig handles PUT /api/v1/feed/config
func UpdateFeedConfig(c *gin.Context) {
	var cfg models.FeedConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid config"})
		return
	}

	f := feed.GlobalFeed()
	f.UpdateConfig(cfg)

	currentCfg := config.Current()
	if currentCfg != nil {
		currentCfg.Feed.Enabled = cfg.Enabled
		currentCfg.Feed.Title = cfg.Title
		currentCfg.Feed.MaxItems = cfg.MaxItems
		currentCfg.Feed.PublicFeed = cfg.PublicFeed
	}

	c.JSON(http.StatusOK, f.GetConfig())
}
