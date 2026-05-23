package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/oauth2"

	"asika/common/auth"
	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
)

var (
	oidcStates   = make(map[string]time.Time)
	oidcStatesMu sync.Mutex
)

func cleanupOIDCStates() {
	now := time.Now()
	oidcStatesMu.Lock()
	defer oidcStatesMu.Unlock()
	for state, created := range oidcStates {
		if now.Sub(created) > 5*time.Minute {
			delete(oidcStates, state)
		}
	}
}

func OIDCLogin(c *gin.Context) {
	providerName := c.Param("provider")
	cfg := config.Current()
	if cfg == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "server not initialized"})
		return
	}

	var provider *models.OIDCProvider
	for i := range cfg.Auth.OIDCProviders {
		if cfg.Auth.OIDCProviders[i].Name == providerName {
			provider = &cfg.Auth.OIDCProviders[i]
			break
		}
	}
	if provider == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "unknown provider"})
		return
	}

	state, err := generateOIDCState()
	if err != nil {
		slog.Error("failed to generate OIDC state", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	oidcStatesMu.Lock()
	oidcStates[state] = time.Now()
	oidcStatesMu.Unlock()
	go cleanupOIDCStates()

	oauth2Config := &oauth2.Config{
		ClientID:     provider.ClientID,
		ClientSecret: provider.ClientSecret,
		RedirectURL:  getRedirectURL(c, providerName),
		Scopes:       provider.Scopes,
	}

	if provider.AuthURL != "" && provider.TokenURL != "" {
		oauth2Config.Endpoint = oauth2.Endpoint{
			AuthURL:  provider.AuthURL,
			TokenURL: provider.TokenURL,
		}
	} else if provider.IssuerURL != "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "OIDC provider requires auth_url and token_url to be set; issuer-based discovery is not implemented"})
		return
	} else {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "OIDC provider has no endpoint configuration"})
		return
	}

	url := oauth2Config.AuthCodeURL(state)
	c.Redirect(http.StatusFound, url)
}

func OIDCCallback(c *gin.Context) {
	providerName := c.Param("provider")
	code := c.Query("code")
	state := c.Query("state")

	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing authorization code"})
		return
	}

	if state == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing state"})
		return
	}

	oidcStatesMu.Lock()
	created, ok := oidcStates[state]
	if !ok || time.Since(created) > 5*time.Minute {
		oidcStatesMu.Unlock()
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid or expired state"})
		return
	}
	delete(oidcStates, state)
	oidcStatesMu.Unlock()

	cfg := config.Current()
	if cfg == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "server not initialized"})
		return
	}

	var provider *models.OIDCProvider
	for i := range cfg.Auth.OIDCProviders {
		if cfg.Auth.OIDCProviders[i].Name == providerName {
			provider = &cfg.Auth.OIDCProviders[i]
			break
		}
	}
	if provider == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "unknown provider"})
		return
	}

	oauth2Config := &oauth2.Config{
		ClientID:     provider.ClientID,
		ClientSecret: provider.ClientSecret,
		RedirectURL:  getRedirectURL(c, providerName),
		Scopes:       provider.Scopes,
		Endpoint: oauth2.Endpoint{
			AuthURL:  provider.AuthURL,
			TokenURL: provider.TokenURL,
		},
	}

	token, err := oauth2Config.Exchange(c, code)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "failed to exchange token"})
		return
	}

	userInfo, err := fetchUserInfo(c, oauth2Config, token, provider.UserInfoURL)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "failed to fetch user info"})
		return
	}

	subject, _ := userInfo["sub"].(string)
	if subject == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing subject in user info"})
		return
	}

	email, _ := userInfo["email"].(string)
	username, _ := userInfo["preferred_username"].(string)
	if username == "" {
		username = email
	}
	if username == "" {
		username = subject
	}

	link, err := db.GetOIDCLink(providerName, subject)
	if err == nil && link != nil {
		link.LastUsedAt = time.Now()
		db.PutOIDCLink(link)

		sessionID := auth.GenerateSessionID()
		now := time.Now()
		expiry := config.GenerateTokenExpiry(cfg.Auth.TokenExpiry)
		session := &models.Session{
			ID:         sessionID,
			Username:   link.Username,
			IssuedAt:   now,
			LastUsedAt: now,
			ExpiresAt:  now.Add(expiry),
			IPAddress:  c.ClientIP(),
			UserAgent:  c.Request.UserAgent(),
		}
		db.PutSession(session)

		jwtToken, _ := auth.GenerateJWTWithSession(link.Username, getUserRole(link.Username, cfg), sessionID)
		c.SetSameSite(http.SameSiteLaxMode)
		c.SetCookie("asika_token", jwtToken, int(expiry.Seconds()), "/", "", true, true)
		c.JSON(http.StatusOK, gin.H{"token": jwtToken, "username": link.Username})
		return
	}

	if !provider.AutoCreate {
		c.JSON(http.StatusForbidden, gin.H{"error": "no linked account. Please link your OIDC account first."})
		return
	}

	defaultRole := provider.DefaultRole
	if defaultRole == "" {
		defaultRole = "operator"
	}

	existingData, err := db.Get(db.BucketUsers, username)
	if err == nil && existingData != nil {
		var existingUser models.User
		if json.Unmarshal(existingData, &existingUser) == nil && existingUser.PasswordHash != "" {
			c.JSON(http.StatusConflict, gin.H{"error": "username already exists with a local account. Please link manually from the account settings page."})
			return
		}
	}

	user := models.User{
		Username:     username,
		PasswordHash: "",
		Role:         defaultRole,
		CreatedAt:    time.Now(),
	}
	userData, _ := json.Marshal(user)
	if err := db.Put(db.BucketUsers, username, userData); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create user"})
		return
	}

	link = &models.OIDCLink{
		Provider:   providerName,
		Subject:    subject,
		Username:   username,
		CreatedAt:  time.Now(),
		LastUsedAt: time.Now(),
	}
	db.PutOIDCLink(link)

	sessionID := auth.GenerateSessionID()
	now := time.Now()
	expiry := config.GenerateTokenExpiry(cfg.Auth.TokenExpiry)
	session := &models.Session{
		ID:         sessionID,
		Username:   username,
		IssuedAt:   now,
		LastUsedAt: now,
		ExpiresAt:  now.Add(expiry),
		IPAddress:  c.ClientIP(),
		UserAgent:  c.Request.UserAgent(),
	}
	db.PutSession(session)

	jwtToken, _ := auth.GenerateJWTWithSession(username, defaultRole, sessionID)
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("asika_token", jwtToken, int(expiry.Seconds()), "/", "", true, true)
	c.JSON(http.StatusOK, gin.H{"token": jwtToken, "username": username})
}

func GetOIDCProviders(c *gin.Context) {
	cfg := config.Current()
	if cfg == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "server not initialized"})
		return
	}
	type providerInfo struct {
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
	}
	var result []providerInfo
	for _, p := range cfg.Auth.OIDCProviders {
		result = append(result, providerInfo{
			Name:        p.Name,
			DisplayName: p.DisplayName,
		})
	}
	c.JSON(http.StatusOK, result)
}

func UnlinkOIDC(c *gin.Context) {
	username, _ := c.Get("username")
	if username == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	provider := c.Query("provider")
	subject := c.Query("subject")
	if provider == "" || subject == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "provider and subject required"})
		return
	}

	link, err := db.GetOIDCLink(provider, subject)
	if err != nil || link == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "link not found"})
		return
	}

	if link.Username != username.(string) {
		c.JSON(http.StatusForbidden, gin.H{"error": "cannot unlink other users' accounts"})
		return
	}

	if err := db.DeleteOIDCLink(provider, subject); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to unlink account"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "OIDC account unlinked"})
}

func ListOIDCLinks(c *gin.Context) {
	username, _ := c.Get("username")
	if username == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	links, err := db.ListOIDCLinks(username.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list links"})
		return
	}

	type linkView struct {
		Provider   string `json:"provider"`
		Subject    string `json:"subject"`
		CreatedAt  string `json:"created_at"`
		LastUsedAt string `json:"last_used_at"`
	}

	var result []linkView
	for _, l := range links {
		result = append(result, linkView{
			Provider:   l.Provider,
			Subject:    l.Subject,
			CreatedAt:  l.CreatedAt.Format("2006-01-02 15:04:05"),
			LastUsedAt: l.LastUsedAt.Format("2006-01-02 15:04:05"),
		})
	}

	c.JSON(http.StatusOK, result)
}

func generateOIDCState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate OIDC state: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func getRedirectURL(c *gin.Context, providerName string) string {
	scheme := "http"
	if c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s/api/v1/auth/oidc/callback/%s", scheme, c.Request.Host, providerName)
}

func fetchUserInfo(c *gin.Context, oauth2Config *oauth2.Config, token *oauth2.Token, userInfoURL string) (map[string]interface{}, error) {
	if userInfoURL == "" {
		return nil, fmt.Errorf("no user info URL configured")
	}
	client := oauth2Config.Client(c, token)
	resp, err := client.Get(userInfoURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

func getUserRole(username string, cfg *models.Config) string {
	data, err := db.Get(db.BucketUsers, username)
	if err != nil {
		return "operator"
	}
	var user models.User
	if err := json.Unmarshal(data, &user); err != nil {
		return "operator"
	}
	return user.Role
}
