package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"asika/common/notifier"
)

func findSMTPNotifier(cfg *models.Config) *notifier.SMTPNotifier {
	for _, nc := range cfg.Notify {
		if nc.Type == "smtp" {
			n := notifier.NewSMTPNotifier(nc.Config)
			if n != nil {
				return n
			}
		}
	}
	return nil
}

func generateResetToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func ForgotPassword(c *gin.Context) {
	cfg := config.Current()
	if cfg == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "server not initialized"})
		return
	}

	smtp := findSMTPNotifier(cfg)
	if smtp == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "SMTP not configured. Please contact the administrator to configure SMTP for password recovery."})
		return
	}

	var req struct {
		Username string `json:"username"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Username == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username required"})
		return
	}

	data, err := db.Get(db.BucketUsers, req.Username)
	if err != nil || data == nil {
		c.JSON(http.StatusOK, gin.H{"message": "If the username exists and has an email configured, a password reset link has been sent."})
		return
	}

	var user models.User
	if err := json.Unmarshal(data, &user); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "If the username exists and has an email configured, a password reset link has been sent."})
		return
	}

	if user.Email == "" {
		c.JSON(http.StatusOK, gin.H{"message": "If the username exists and has an email configured, a password reset link has been sent."})
		return
	}

	token, err := generateResetToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate reset token"})
		return
	}

	expiresAt := time.Now().Add(15 * time.Minute)
	if err := db.PutPasswordResetToken(token, user.Username, expiresAt); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save reset token"})
		return
	}

	resetURL := "/reset-password?token=" + url.QueryEscape(token)
	emailBody := fmt.Sprintf("Hello %s,\n\nYou requested a password reset for your Asika account.\n\nClick the link below to set a new password:\n%s\n\nThis link expires in 15 minutes.\n\nIf you did not request this, please ignore this email.\n", user.Username, resetURL)

	go func() {
		if err := smtp.Send(c.Request.Context(), "Asika Password Reset", emailBody); err != nil {
			return
		}
	}()

	c.JSON(http.StatusOK, gin.H{"message": "If the username exists and has an email configured, a password reset link has been sent."})
}

func ResetPassword(c *gin.Context) {
	var req struct {
		Token       string `json:"token"`
		NewPassword string `json:"new_password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Token == "" || req.NewPassword == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "token and new_password required"})
		return
	}

	if len(req.NewPassword) < 8 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "password must be at least 8 characters"})
		return
	}

	entry, err := db.GetPasswordResetToken(req.Token)
	if err != nil || entry == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid or expired token"})
		return
	}

	if entry.ExpiresAt.Before(time.Now()) {
		db.DeletePasswordResetToken(req.Token)
		c.JSON(http.StatusBadRequest, gin.H{"error": "token expired"})
		return
	}

	if err := db.DeletePasswordResetToken(req.Token); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to consume token"})
		return
	}

	userData, err := db.Get(db.BucketUsers, entry.Username)
	if err != nil || userData == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user not found"})
		return
	}
	var user models.User
	if err := json.Unmarshal(userData, &user); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load user"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash password"})
		return
	}
	user.PasswordHash = string(hash)

	updated, err := json.Marshal(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to marshal user"})
		return
	}
	if err := db.Put(db.BucketUsers, entry.Username, updated); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save user"})
		return
	}

	db.DeleteUserSessions(entry.Username)

	c.JSON(http.StatusOK, gin.H{"message": "password reset successful"})
}
