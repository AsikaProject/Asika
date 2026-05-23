package handlers

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"asika/common/db"
	"asika/common/models"
)

func TOTPStatus(c *gin.Context) {
	username, _ := c.Get("username")
	if username == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	data, err := db.Get(db.BucketUsers, username.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load user"})
		return
	}
	var user models.User
	if err := json.Unmarshal(data, &user); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"enabled": user.TOTPEnabled,
	})
}

func EnrollTOTP(c *gin.Context) {
	username, _ := c.Get("username")
	if username == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	data, err := db.Get(db.BucketUsers, username.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load user"})
		return
	}
	var user models.User
	if err := json.Unmarshal(data, &user); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	if user.TOTPEnabled {
		c.JSON(http.StatusBadRequest, gin.H{"error": "2FA already enabled"})
		return
	}

	secret := make([]byte, 20)
	if _, err := rand.Read(secret); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate TOTP secret"})
		return
	}
	secretBase32 := base32.StdEncoding.EncodeToString(secret)

	user.TOTPSecret = secretBase32
	updated, _ := json.Marshal(user)
	if err := db.Put(db.BucketUsers, user.Username, updated); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save secret"})
		return
	}

	otpURL := fmt.Sprintf("otpauth://totp/Asika:%s?secret=%s&issuer=Asika", user.Username, secretBase32)

	c.JSON(http.StatusOK, gin.H{
		"secret": secretBase32,
		"url":    otpURL,
	})
}

func VerifyTOTP(c *gin.Context) {
	username, _ := c.Get("username")
	if username == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req struct {
		Code string `json:"code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "code required"})
		return
	}

	data, err := db.Get(db.BucketUsers, username.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load user"})
		return
	}
	var user models.User
	if err := json.Unmarshal(data, &user); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	if !validateTOTPCode(user.TOTPSecret, req.Code) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid code"})
		return
	}

	plainCodes, err := generatePlainBackupCodes()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate backup codes"})
		return
	}
	user.TOTPEnabled = true
	user.BackupCodes = hashBackupCodes(plainCodes)

	updated, _ := json.Marshal(user)
	if err := db.Put(db.BucketUsers, user.Username, updated); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to enable 2FA"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":      "2FA enabled",
		"backup_codes": plainCodes,
	})
}

func DisableTOTP(c *gin.Context) {
	username, _ := c.Get("username")
	if username == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req struct {
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "password required"})
		return
	}

	data, err := db.Get(db.BucketUsers, username.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load user"})
		return
	}
	var user models.User
	if err := json.Unmarshal(data, &user); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid password"})
		return
	}

	user.TOTPSecret = ""
	user.TOTPEnabled = false
	user.BackupCodes = nil

	updated, _ := json.Marshal(user)
	if err := db.Put(db.BucketUsers, user.Username, updated); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to disable 2FA"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "2FA disabled"})
}

func RegenerateBackupCodes(c *gin.Context) {
	username, _ := c.Get("username")
	if username == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req struct {
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "password required"})
		return
	}

	data, err := db.Get(db.BucketUsers, username.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load user"})
		return
	}
	var user models.User
	if err := json.Unmarshal(data, &user); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid password"})
		return
	}

	plainCodes, err := generatePlainBackupCodes()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate backup codes"})
		return
	}
	user.BackupCodes = hashBackupCodes(plainCodes)

	updated, _ := json.Marshal(user)
	if err := db.Put(db.BucketUsers, user.Username, updated); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to regenerate codes"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"backup_codes": plainCodes,
	})
}

func validateTOTPCode(secret, code string) bool {
	if secret == "" || code == "" {
		return false
	}
	secret = strings.ToUpper(strings.ReplaceAll(secret, " ", ""))
	key, err := base32.StdEncoding.DecodeString(secret)
	if err != nil {
		return false
	}
	now := time.Now().Unix()
	for offset := int64(-1); offset <= 1; offset++ {
		counter := uint64((now + offset*30) / 30)
		if generateTOTPCode(key, counter) == code {
			return true
		}
	}
	return false
}

func generateTOTPCode(key []byte, counter uint64) string {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, counter)
	mac := hmac.New(sha1.New, key)
	mac.Write(buf)
	hash := mac.Sum(nil)
	offset := hash[len(hash)-1] & 0x0F
	binaryCode := int64(hash[offset]&0x7F)<<24 | int64(hash[offset+1])<<16 | int64(hash[offset+2])<<8 | int64(hash[offset+3])
	otp := binaryCode % 1000000
	return fmt.Sprintf("%06d", otp)
}

func generatePlainBackupCodes() ([]string, error) {
	codes := make([]string, 10)
	for i := 0; i < 10; i++ {
		b := make([]byte, 4)
		if _, err := rand.Read(b); err != nil {
			return nil, fmt.Errorf("failed to generate backup code: %w", err)
		}
		codes[i] = fmt.Sprintf("%02x%02x-%02x%02x", b[0], b[1], b[2], b[3])
	}
	return codes, nil
}

func generateAndHashBackupCodes() ([]string, error) {
	plain, err := generatePlainBackupCodes()
	if err != nil {
		return nil, err
	}
	return hashBackupCodes(plain), nil
}

func hashBackupCodes(plain []string) []string {
	hashed := make([]string, len(plain))
	for i, code := range plain {
		hash, _ := bcrypt.GenerateFromPassword([]byte(code), bcrypt.DefaultCost)
		hashed[i] = string(hash)
	}
	return hashed
}
