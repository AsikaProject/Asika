package feed

import (
	"encoding/xml"
	"fmt"
	"sort"
	"sync"
	"time"

	"asika/common/events"
	"asika/common/models"
)

// DefaultFeedConfig returns the default feed configuration.
func DefaultFeedConfig() models.FeedConfig {
	return models.FeedConfig{
		Enabled:    true,
		Title:      "Asika PR Feed",
		MaxItems:   50,
		PublicFeed: false,
	}
}

// FeedItem represents a single feed entry derived from a PR event.
type FeedItem struct {
	Title       string    `json:"title"`
	Link        string    `json:"link"`
	Description string    `json:"description"`
	Author      string    `json:"author"`
	PubDate     time.Time `json:"pub_date"`
	GUID        string    `json:"guid"`
	RepoGroup   string    `json:"repo_group"`
	Platform    string    `json:"platform"`
	PRNumber    int       `json:"pr_number"`
}

// RSS represents an RSS 2.0 feed.
type RSS struct {
	XMLName xml.Name   `xml:"rss"`
	Version string     `xml:"version,attr"`
	Channel RSSChannel `xml:"channel"`
}

// RSSChannel represents the RSS channel element.
type RSSChannel struct {
	Title       string    `xml:"title"`
	Link        string    `xml:"link"`
	Description string    `xml:"description"`
	Items       []RSSItem `xml:"item"`
}

// RSSItem represents an RSS item element.
type RSSItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	Author      string `xml:"author,omitempty"`
	PubDate     string `xml:"pubDate,omitempty"`
	GUID        string `xml:"guid"`
}

// Feed maintains an in-memory ring buffer of recent PR events and generates RSS feeds.
type Feed struct {
	mu       sync.RWMutex
	items    []FeedItem
	maxItems int
	enabled  bool
	title    string
}

// NewFeed creates a new Feed instance.
func NewFeed(cfg models.FeedConfig) *Feed {
	maxItems := cfg.MaxItems
	if maxItems <= 0 {
		maxItems = 50
	}
	return &Feed{
		items:    make([]FeedItem, 0, maxItems),
		maxItems: maxItems,
		enabled:  cfg.Enabled,
		title:    cfg.Title,
	}
}

// UpdateConfig updates the feed configuration at runtime.
func (f *Feed) UpdateConfig(cfg models.FeedConfig) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.enabled = cfg.Enabled
	f.title = cfg.Title
	if cfg.MaxItems > 0 {
		f.maxItems = cfg.MaxItems
	}
}

// AddEvent adds a PR event to the feed.
func (f *Feed) AddEvent(eventType events.EventType, pr *models.PRRecord) {
	if pr == nil {
		return
	}

	item := FeedItem{
		Title:     fmt.Sprintf("[%s] %s - PR #%d", actionLabel(eventType), pr.Title, pr.PRNumber),
		Link:      pr.HTMLURL,
		Author:    pr.Author,
		PubDate:   time.Now(),
		GUID:      fmt.Sprintf("%s/%s/%d/%s", pr.RepoGroup, pr.Platform, pr.PRNumber, eventType),
		RepoGroup: pr.RepoGroup,
		Platform:  pr.Platform,
		PRNumber:  pr.PRNumber,
	}

	switch eventType {
	case events.EventPROpened:
		item.Description = fmt.Sprintf("PR #%d opened by %s in %s/%s", pr.PRNumber, pr.Author, pr.RepoGroup, pr.Platform)
	case events.EventPRMerged:
		item.Description = fmt.Sprintf("PR #%d merged in %s/%s", pr.PRNumber, pr.RepoGroup, pr.Platform)
	case events.EventPRClosed:
		item.Description = fmt.Sprintf("PR #%d closed in %s/%s", pr.PRNumber, pr.RepoGroup, pr.Platform)
	case events.EventPRApproved:
		item.Description = fmt.Sprintf("PR #%d approved in %s/%s", pr.PRNumber, pr.RepoGroup, pr.Platform)
	case events.EventPRReopened:
		item.Description = fmt.Sprintf("PR #%d reopened in %s/%s", pr.PRNumber, pr.RepoGroup, pr.Platform)
	default:
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.items = append(f.items, item)
	if len(f.items) > f.maxItems {
		f.items = f.items[len(f.items)-f.maxItems:]
	}
}

// GetItems returns a copy of the current feed items.
func (f *Feed) GetItems() []FeedItem {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return itemsSorted(f.items)
}

// GetItemsForRepoGroup returns feed items filtered by repo group.
func (f *Feed) GetItemsForRepoGroup(repoGroup string) []FeedItem {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var filtered []FeedItem
	for _, item := range f.items {
		if item.RepoGroup == repoGroup {
			filtered = append(filtered, item)
		}
	}
	return itemsSorted(filtered)
}

// GenerateRSS generates an RSS 2.0 XML feed from the current items.
func (f *Feed) GenerateRSS(items []FeedItem, baseURL string) ([]byte, error) {
	f.mu.RLock()
	title := f.title
	f.mu.RUnlock()

	channel := RSSChannel{
		Title:       title,
		Link:        baseURL,
		Description: "Asika PR Activity Feed",
		Items:       make([]RSSItem, 0, len(items)),
	}

	for _, item := range items {
		channel.Items = append(channel.Items, RSSItem{
			Title:       item.Title,
			Link:        item.Link,
			Description: item.Description,
			Author:      item.Author,
			PubDate:     item.PubDate.Format(time.RFC1123Z),
			GUID:        item.GUID,
		})
	}

	rss := RSS{
		Version: "2.0",
		Channel: channel,
	}

	output, err := xml.MarshalIndent(rss, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal RSS: %w", err)
	}

	return append([]byte(xml.Header), output...), nil
}

// GetConfig returns the current feed configuration.
func (f *Feed) GetConfig() models.FeedConfig {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return models.FeedConfig{
		Enabled: f.enabled,
		Title:   f.title,
	}
}

func actionLabel(eventType events.EventType) string {
	switch eventType {
	case events.EventPROpened:
		return "OPENED"
	case events.EventPRMerged:
		return "MERGED"
	case events.EventPRClosed:
		return "CLOSED"
	case events.EventPRApproved:
		return "APPROVED"
	case events.EventPRReopened:
		return "REOPENED"
	default:
		return string(eventType)
	}
}

func itemsSorted(items []FeedItem) []FeedItem {
	sorted := make([]FeedItem, len(items))
	copy(sorted, items)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].PubDate.After(sorted[j].PubDate)
	})
	return sorted
}

var globalFeed *Feed
var globalFeedMu sync.Mutex

// InitGlobalFeed initializes the global feed instance.
func InitGlobalFeed(cfg models.FeedConfig) {
	globalFeedMu.Lock()
	defer globalFeedMu.Unlock()
	globalFeed = NewFeed(cfg)
}

// GlobalFeed returns the global feed instance.
func GlobalFeed() *Feed {
	globalFeedMu.Lock()
	defer globalFeedMu.Unlock()
	if globalFeed == nil {
		globalFeed = NewFeed(DefaultFeedConfig())
	}
	return globalFeed
}

// StartFeedSubscriber subscribes to the event bus and feeds events into the global feed.
func StartFeedSubscriber() {
	go func() {
		ch := events.Subscribe()
		for event := range ch {
			switch event.Type {
			case events.EventPROpened, events.EventPRMerged, events.EventPRClosed,
				events.EventPRApproved, events.EventPRReopened:
				GlobalFeed().AddEvent(event.Type, event.PR)
			}
		}
	}()
}
