package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
)

//go:embed locales/*
var localeFS embed.FS

var (
	mu       sync.RWMutex
	current  = "en"
	messages = map[string]map[string]string{
		"en": {},
	}
)

func init() {
	entries, err := localeFS.ReadDir("locales")
	if err != nil {
		slog.Warn("failed to read embedded locales directory", "error", err)
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := localeFS.ReadFile("locales/" + entry.Name())
		if err != nil {
			slog.Warn("failed to read embedded locale file", "file", entry.Name(), "error", err)
			continue
		}
		var msgs map[string]string
		if err := json.Unmarshal(data, &msgs); err != nil {
			slog.Warn("failed to parse embedded locale file", "file", entry.Name(), "error", err)
			continue
		}
		locale := strings.TrimSuffix(entry.Name(), ".json")
		messages[locale] = msgs
		slog.Info("locale loaded", "locale", locale, "keys", len(msgs))
	}
}

// SetLocale sets the current locale.
func SetLocale(locale string) {
	mu.Lock()
	defer mu.Unlock()
	if _, ok := messages[locale]; !ok {
		slog.Warn("locale not found, falling back to en", "locale", locale)
		locale = "en"
	}
	current = locale
}

// Locale returns the current locale.
func Locale() string {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

// LoadLocale loads translations from a JSON file for the given locale.
// File format: {"key": "translated string", ...}
func LoadLocale(locale, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read locale file %s: %w", path, err)
	}
	var msgs map[string]string
	if err := json.Unmarshal(data, &msgs); err != nil {
		return fmt.Errorf("failed to parse locale file %s: %w", path, err)
	}
	mu.Lock()
	defer mu.Unlock()
	messages[locale] = msgs
	slog.Info("locale loaded", "locale", locale, "keys", len(msgs))
	return nil
}

// Register adds translation strings for a locale programmatically.
func Register(locale string, msgs map[string]string) {
	mu.Lock()
	defer mu.Unlock()
	if _, ok := messages[locale]; !ok {
		messages[locale] = make(map[string]string)
	}
	for k, v := range msgs {
		messages[locale][k] = v
	}
}

// T translates a key to the current locale.
// Supports fmt.Sprintf-style args.
func T(key string, args ...interface{}) string {
	mu.RLock()
	defer mu.RUnlock()
	locale := current
	msg, ok := messages[locale][key]
	if !ok {
		// Fallback to en
		if locale != "en" {
			if msg, ok = messages["en"][key]; ok {
				return fmt.Sprintf(msg, args...)
			}
		}
		return key
	}
	return fmt.Sprintf(msg, args...)
}

// ParseAcceptLanguage parses an Accept-Language header and returns the best match.
func ParseAcceptLanguage(header string) string {
	if header == "" {
		return "en"
	}
	// Simple: take first language tag
	parts := strings.Split(header, ",")
	for _, part := range parts {
		lang := strings.TrimSpace(strings.Split(part, ";")[0])
		lang = strings.ToLower(lang)
		// Map zh, zh-CN, zh-TW -> zh
		if strings.HasPrefix(lang, "zh") {
			return "zh"
		}
		if strings.HasPrefix(lang, "en") {
			return "en"
		}
	}
	return "en"
}
