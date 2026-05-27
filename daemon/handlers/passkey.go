package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-webauthn/webauthn/webauthn"

	"asika/common/auth"
	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
)

var webAuthn *webauthn.WebAuthn

// InitWebAuthn initializes the WebAuthn instance
func InitWebAuthn() error {
	cfg := config.Current()
	if cfg == nil || !cfg.Auth.PasskeyEnabled {
		return nil
	}

	rpID := cfg.Auth.PasskeyRPID
	rpName := cfg.Auth.PasskeyRPName
	rpOrigin := cfg.Auth.PasskeyRPOrigin

	if rpID == "" || rpName == "" || rpOrigin == "" {
		return fmt.Errorf("passkey configuration incomplete: rp_id, rp_name, and rp_origin are required")
	}

	wconfig := &webauthn.Config{
		RPDisplayName: rpName,
		RPID:          rpID,
		RPOrigins:     []string{rpOrigin},
	}

	var err error
	webAuthn, err = webauthn.New(wconfig)
	if err != nil {
		return fmt.Errorf("failed to initialize WebAuthn: %w", err)
	}

	slog.Info("WebAuthn initialized", "rp_id", rpID, "rp_name", rpName)
	return nil
}

// WebAuthnUser implements the webauthn.User interface
type WebAuthnUser struct {
	Username    string
	Credentials []webauthn.Credential
}

func (u *WebAuthnUser) WebAuthnID() []byte {
	return []byte(u.Username)
}

func (u *WebAuthnUser) WebAuthnName() string {
	return u.Username
}

func (u *WebAuthnUser) WebAuthnDisplayName() string {
	return u.Username
}

func (u *WebAuthnUser) WebAuthnCredentials() []webauthn.Credential {
	return u.Credentials
}

func (u *WebAuthnUser) WebAuthnIcon() string {
	return ""
}

// getWebAuthnUser loads a user's WebAuthn credentials
func getWebAuthnUser(username string) (*WebAuthnUser, error) {
	var credentials []webauthn.Credential

	err := db.ForEach(db.BucketWebAuthnCredentials, func(key, value []byte) error {
		var cred models.WebAuthnCredential
		if err := json.Unmarshal(value, &cred); err != nil {
			return nil
		}
		if cred.UserID == username {
			credentials = append(credentials, webauthn.Credential{
				ID:              cred.CredentialID,
				PublicKey:       cred.PublicKey,
				Authenticator: webauthn.Authenticator{
					SignCount: cred.SignCount,
					AAGUID:    cred.AAGUID,
				},
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &WebAuthnUser{
		Username:    username,
		Credentials: credentials,
	}, nil
}

// saveWebAuthnCredential saves a WebAuthn credential
func saveWebAuthnCredential(username string, cred *webauthn.Credential, name string) error {
	credModel := models.WebAuthnCredential{
		ID:           fmt.Sprintf("%s:%x", username, cred.ID),
		UserID:       username,
		CredentialID: cred.ID,
		PublicKey:    cred.PublicKey,
		SignCount:    cred.Authenticator.SignCount,
		AAGUID:       cred.Authenticator.AAGUID,
		CreatedAt:    time.Now(),
		LastUsedAt:   time.Now(),
		Name:         name,
	}

	data, err := json.Marshal(credModel)
	if err != nil {
		return err
	}

	return db.Put(db.BucketWebAuthnCredentials, credModel.ID, data)
}

// updateWebAuthnCredentialSignCount updates the sign count for a credential
func updateWebAuthnCredentialSignCount(credID []byte, newCount uint32) error {
	var found bool
	err := db.ForEach(db.BucketWebAuthnCredentials, func(key, value []byte) error {
		var cred models.WebAuthnCredential
		if err := json.Unmarshal(value, &cred); err != nil {
			return nil
		}
		if string(cred.CredentialID) == string(credID) {
			cred.SignCount = newCount
			cred.LastUsedAt = time.Now()
			data, err := json.Marshal(cred)
			if err != nil {
				return err
			}
			found = true
			return db.Put(db.BucketWebAuthnCredentials, string(key), data)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("credential not found")
	}
	return nil
}

// PasskeyRegisterBegin handles POST /api/v1/auth/passkey/register/begin
func PasskeyRegisterBegin(c *gin.Context) {
	if webAuthn == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Passkey authentication is not enabled"})
		return
	}

	username := c.GetString("username")
	if username == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication required"})
		return
	}

	user, err := getWebAuthnUser(username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load user"})
		return
	}

	options, sessionData, err := webAuthn.BeginRegistration(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to begin registration"})
		return
	}

	// Store session data
	sessionBytes, err := json.Marshal(sessionData)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to serialize session"})
		return
	}

	sessionID := auth.GenerateSessionID()
	session := models.WebAuthnSession{
		ID:        sessionID,
		UserID:    username,
		Challenge: string(sessionBytes),
		CreatedAt: time.Now(),
	}

	sessionDataBytes, _ := json.Marshal(session)
	if err := db.Put(db.BucketWebAuthnCredentials, "session:"+sessionID, sessionDataBytes); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to store session"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"options":   options,
		"session_id": sessionID,
	})
}

// PasskeyRegisterFinish handles POST /api/v1/auth/passkey/register/finish
func PasskeyRegisterFinish(c *gin.Context) {
	if webAuthn == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Passkey authentication is not enabled"})
		return
	}

	username := c.GetString("username")
	if username == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication required"})
		return
	}

	var req struct {
		SessionID string          `json:"session_id"`
		Name      string          `json:"name"`
		Response  json.RawMessage `json:"response"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	// Load session data
	sessionDataBytes, err := db.Get(db.BucketWebAuthnCredentials, "session:"+req.SessionID)
	if err != nil || sessionDataBytes == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid or expired session"})
		return
	}

	var session models.WebAuthnSession
	if err := json.Unmarshal(sessionDataBytes, &session); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse session"})
		return
	}

	if session.UserID != username {
		c.JSON(http.StatusForbidden, gin.H{"error": "Session user mismatch"})
		return
	}

	// Parse session data
	var sessionData webauthn.SessionData
	if err := json.Unmarshal([]byte(session.Challenge), &sessionData); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse session data"})
		return
	}

	user, err := getWebAuthnUser(username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load user"})
		return
	}

	credential, err := webAuthn.FinishRegistration(user, sessionData, c.Request)
	if err != nil {
		slog.Error("passkey registration failed", "error", err, "username", username)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Registration failed"})
		return
	}

	// Save credential
	credName := req.Name
	if credName == "" {
		credName = "Passkey"
	}
	if err := saveWebAuthnCredential(username, credential, credName); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save credential"})
		return
	}

	// Clean up session
	db.Delete(db.BucketWebAuthnCredentials, "session:"+req.SessionID)

	slog.Info("passkey registered", "username", username, "name", credName)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// PasskeyLoginBegin handles POST /api/v1/auth/passkey/login/begin
func PasskeyLoginBegin(c *gin.Context) {
	if webAuthn == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Passkey authentication is not enabled"})
		return
	}

	var req struct {
		Username string `json:"username"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	if req.Username == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Username is required"})
		return
	}

	user, err := getWebAuthnUser(req.Username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load user"})
		return
	}

	if len(user.Credentials) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No passkeys registered for this user"})
		return
	}

	options, sessionData, err := webAuthn.BeginLogin(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to begin login"})
		return
	}

	// Store session data
	sessionBytes, err := json.Marshal(sessionData)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to serialize session"})
		return
	}

	sessionID := auth.GenerateSessionID()
	session := models.WebAuthnSession{
		ID:        sessionID,
		UserID:    req.Username,
		Challenge: string(sessionBytes),
		CreatedAt: time.Now(),
	}

	sessionDataBytes, _ := json.Marshal(session)
	if err := db.Put(db.BucketWebAuthnCredentials, "session:"+sessionID, sessionDataBytes); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to store session"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"options":    options,
		"session_id": sessionID,
	})
}

// PasskeyLoginFinish handles POST /api/v1/auth/passkey/login/finish
func PasskeyLoginFinish(c *gin.Context) {
	if webAuthn == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Passkey authentication is not enabled"})
		return
	}

	var req struct {
		SessionID string          `json:"session_id"`
		Response  json.RawMessage `json:"response"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	// Load session data
	sessionDataBytes, err := db.Get(db.BucketWebAuthnCredentials, "session:"+req.SessionID)
	if err != nil || sessionDataBytes == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid or expired session"})
		return
	}

	var session models.WebAuthnSession
	if err := json.Unmarshal(sessionDataBytes, &session); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse session"})
		return
	}

	// Parse session data
	var sessionData webauthn.SessionData
	if err := json.Unmarshal([]byte(session.Challenge), &sessionData); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse session data"})
		return
	}

	user, err := getWebAuthnUser(session.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load user"})
		return
	}

	credential, err := webAuthn.FinishLogin(user, sessionData, c.Request)
	if err != nil {
		slog.Error("passkey login failed", "error", err, "username", session.UserID)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
		return
	}

	// Update sign count
	if err := updateWebAuthnCredentialSignCount(credential.ID, credential.Authenticator.SignCount); err != nil {
		slog.Error("failed to update sign count", "error", err)
	}

	// Clean up session
	db.Delete(db.BucketWebAuthnCredentials, "session:"+req.SessionID)

	// Load user from DB to get role
	var userRecord models.User
	userData, err := db.Get(db.BucketUsers, session.UserID)
	if err != nil || userData == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "User not found"})
		return
	}
	if err := json.Unmarshal(userData, &userRecord); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse user"})
		return
	}

	// Generate JWT
	token, err := auth.GenerateJWTWithSession(userRecord.Username, userRecord.Role, auth.GenerateSessionID())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
		return
	}

	// Set cookie
	c.SetCookie("asika_token", token, int(auth.GetTokenExpiry().Seconds()), "/", "", false, true)

	slog.Info("passkey login successful", "username", session.UserID)
	c.JSON(http.StatusOK, gin.H{
		"token":    token,
		"username": userRecord.Username,
		"role":     userRecord.Role,
	})
}

// ListPasskeyCredentials handles GET /api/v1/auth/passkey/credentials
func ListPasskeyCredentials(c *gin.Context) {
	username := c.GetString("username")
	if username == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication required"})
		return
	}

	var credentials []models.WebAuthnCredential
	err := db.ForEach(db.BucketWebAuthnCredentials, func(key, value []byte) error {
		var cred models.WebAuthnCredential
		if err := json.Unmarshal(value, &cred); err != nil {
			return nil
		}
		if cred.UserID == username {
			// Don't send credential bytes to client
			cred.CredentialID = nil
			cred.PublicKey = nil
			cred.AAGUID = nil
			credentials = append(credentials, cred)
		}
		return nil
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list credentials"})
		return
	}

	c.JSON(http.StatusOK, credentials)
}

// DeletePasskeyCredential handles DELETE /api/v1/auth/passkey/credentials/:id
func DeletePasskeyCredential(c *gin.Context) {
	username := c.GetString("username")
	if username == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication required"})
		return
	}

	credID := c.Param("id")
	if credID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Credential ID is required"})
		return
	}

	// Verify the credential belongs to the user
	data, err := db.Get(db.BucketWebAuthnCredentials, credID)
	if err != nil || data == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Credential not found"})
		return
	}

	var cred models.WebAuthnCredential
	if err := json.Unmarshal(data, &cred); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse credential"})
		return
	}

	if cred.UserID != username {
		c.JSON(http.StatusForbidden, gin.H{"error": "Credential does not belong to user"})
		return
	}

	if err := db.Delete(db.BucketWebAuthnCredentials, credID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete credential"})
		return
	}

	slog.Info("passkey deleted", "username", username, "credential_id", credID)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
