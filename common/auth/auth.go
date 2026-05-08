package auth

import (
    "errors"
    "sync"
    "time"

    "github.com/golang-jwt/jwt/v5"
    "golang.org/x/crypto/bcrypt"
)

var (
    jwtSecret   []byte
    tokenExpiry time.Duration
    blacklistMu sync.RWMutex
    blacklist   = make(map[string]time.Time)
)

// Init initializes the auth module
func Init(secret string, expiry time.Duration) {
    jwtSecret = []byte(secret)
    tokenExpiry = expiry
}

// HashPassword hashes a password using bcrypt
func HashPassword(password string) (string, error) {
    hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
    if err != nil {
        return "", err
    }
    return string(hash), nil
}

// CheckPassword compares a password with its hash
func CheckPassword(password, hash string) bool {
    err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
    return err == nil
}

// GenerateInternalToken generates a long-lived JWT for internal bot API calls.
func GenerateInternalToken() (string, error) {
	return GenerateJWT("bot", "admin")
}

// GenerateJWT generates a JWT token for a user
func GenerateJWT(username, role string) (string, error) {
    claims := jwt.MapClaims{
        "username": username,
        "role":     role,
        "exp":      time.Now().Add(tokenExpiry).Unix(),
        "iat":      time.Now().Unix(),
    }

    token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
    return token.SignedString(jwtSecret)
}

// ValidateJWT validates a JWT token and returns the claims
func ValidateJWT(tokenString string) (jwt.MapClaims, error) {
    token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
	if token.Method != jwt.SigningMethodHS256 {
			return nil, jwt.ErrSignatureInvalid
		}
        return jwtSecret, nil
    })

    if err != nil {
        return nil, err
    }

    if !token.Valid {
        return nil, jwt.ErrSignatureInvalid
    }

    claims, ok := token.Claims.(jwt.MapClaims)
    if !ok {
        return nil, jwt.ErrInvalidKey
    }

    // Check if token is blacklisted
    blacklistMu.RLock()
    _, blacklisted := blacklist[tokenString]
    blacklistMu.RUnlock()
    if blacklisted {
        return nil, errors.New("token is blacklisted")
    }

    return claims, nil
}

// BlacklistToken adds a token to the blacklist
func BlacklistToken(tokenString string) {
    blacklistMu.Lock()
    blacklist[tokenString] = time.Now()
    blacklistMu.Unlock()
}

// CleanupBlacklist removes expired tokens from blacklist
func CleanupBlacklist() {
    now := time.Now()
    blacklistMu.Lock()
    defer blacklistMu.Unlock()
    for token, addedAt := range blacklist {
        // Remove tokens older than 2x expiry time
        if now.Sub(addedAt) > tokenExpiry*2 {
            delete(blacklist, token)
        }
    }
}

// GetUserRole returns the role from JWT claims
func GetUserRole(claims jwt.MapClaims) string {
    if role, ok := claims["role"].(string); ok {
        return role
    }
    return ""
}

// GetUsername returns the username from JWT claims
func GetUsername(claims jwt.MapClaims) string {
    if username, ok := claims["username"].(string); ok {
        return username
    }
    return ""
}

// HasPermission checks if a role has permission for an action
func HasPermission(role, required string) bool {
    roleHierarchy := map[string]int{
        "viewer":    1,
        "operator":  2,
        "admin":     3,
    }

    userLevel, ok := roleHierarchy[role]
    if !ok {
        return false
    }

    requiredLevel, ok := roleHierarchy[required]
    if !ok {
        return false
    }

    return userLevel >= requiredLevel
}
