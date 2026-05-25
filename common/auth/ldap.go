package auth

import (
	"crypto/tls"
	"fmt"
	"log/slog"
	"strings"

	"github.com/go-ldap/ldap/v3"

	"asika/common/models"
)

// LDAPAuthenticator handles LDAP/AD authentication.
type LDAPAuthenticator struct {
	cfg models.LDAPConfig
}

// NewLDAPAuthenticator creates a new LDAP authenticator.
func NewLDAPAuthenticator(cfg models.LDAPConfig) *LDAPAuthenticator {
	return &LDAPAuthenticator{cfg: cfg}
}

// Authenticate validates credentials against LDAP and returns user info.
// Returns nil if authentication fails.
func (a *LDAPAuthenticator) Authenticate(username, password string) (*LDAPUserInfo, error) {
	if !a.cfg.Enabled {
		return nil, fmt.Errorf("LDAP authentication is not enabled")
	}

	addr := fmt.Sprintf("%s:%d", a.cfg.Host, a.cfg.Port)

	var conn *ldap.Conn
	var err error

	if a.cfg.UseTLS {
		conn, err = ldap.DialTLS("tcp", addr, &tls.Config{
			ServerName:         a.cfg.Host,
			InsecureSkipVerify: false,
		})
	} else {
		conn, err = ldap.Dial("tcp", addr)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to connect to LDAP server: %w", err)
	}
	defer conn.Close()

	if a.cfg.StartTLS && !a.cfg.UseTLS {
		if err := conn.StartTLS(&tls.Config{ServerName: a.cfg.Host}); err != nil {
			return nil, fmt.Errorf("failed to start TLS: %w", err)
		}
	}

	if a.cfg.BindDN != "" && a.cfg.BindPassword != "" {
		if err := conn.Bind(a.cfg.BindDN, a.cfg.BindPassword); err != nil {
			return nil, fmt.Errorf("LDAP bind failed: %w", err)
		}
	}

	userFilter := strings.ReplaceAll(a.cfg.UserFilter, "{username}", ldap.EscapeFilter(username))
	searchRequest := ldap.NewSearchRequest(
		a.cfg.BaseDN,
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 1, 0, false,
		userFilter,
		[]string{"dn", a.cfg.EmailAttribute, a.cfg.NameAttribute, "cn", "mail", "displayName"},
		nil,
	)

	sr, err := conn.Search(searchRequest)
	if err != nil {
		return nil, fmt.Errorf("LDAP search failed: %w", err)
	}
	if len(sr.Entries) == 0 {
		return nil, fmt.Errorf("user not found in LDAP")
	}
	if len(sr.Entries) > 1 {
		return nil, fmt.Errorf("multiple LDAP entries found for user")
	}

	userEntry := sr.Entries[0]
	userDN := userEntry.DN

	if err := conn.Bind(userDN, password); err != nil {
		return nil, fmt.Errorf("LDAP authentication failed: %w", err)
	}

	info := &LDAPUserInfo{
		Username: username,
		DN:       userDN,
		Email:    getAttr(userEntry, a.cfg.EmailAttribute, "mail"),
		Name:     getAttr(userEntry, a.cfg.NameAttribute, "cn", "displayName"),
	}

	if len(a.cfg.AllowedGroups) > 0 && a.cfg.GroupFilter != "" {
		groupFilter := strings.ReplaceAll(a.cfg.GroupFilter, "{userDN}", ldap.EscapeFilter(userDN))
		groupFilter = strings.ReplaceAll(groupFilter, "{username}", ldap.EscapeFilter(username))

		groupSearch := ldap.NewSearchRequest(
			a.cfg.GroupDN,
			ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
			groupFilter,
			[]string{"cn", "dn"},
			nil,
		)

		groupSr, err := conn.Search(groupSearch)
		if err != nil {
			slog.Warn("LDAP group search failed", "error", err, "username", username)
		} else {
			info.Groups = make([]string, 0, len(groupSr.Entries))
			for _, entry := range groupSr.Entries {
				if cn := entry.GetAttributeValue("cn"); cn != "" {
					info.Groups = append(info.Groups, cn)
				}
			}
		}

		if !a.isInAllowedGroup(info.Groups) {
			return nil, fmt.Errorf("user not in any allowed LDAP group")
		}
	}

	slog.Info("LDAP authentication successful", "username", username, "dn", userDN)
	return info, nil
}

func (a *LDAPAuthenticator) isInAllowedGroup(userGroups []string) bool {
	if len(a.cfg.AllowedGroups) == 0 {
		return true
	}
	for _, allowed := range a.cfg.AllowedGroups {
		for _, g := range userGroups {
			if strings.EqualFold(allowed, g) {
				return true
			}
		}
	}
	return false
}

// LDAPUserInfo holds information about an LDAP-authenticated user.
type LDAPUserInfo struct {
	Username string
	DN       string
	Email    string
	Name     string
	Groups   []string
}

func getAttr(entry *ldap.Entry, primary string, fallbacks ...string) string {
	if primary != "" {
		if v := entry.GetAttributeValue(primary); v != "" {
			return v
		}
	}
	for _, fb := range fallbacks {
		if v := entry.GetAttributeValue(fb); v != "" {
			return v
		}
	}
	return ""
}
