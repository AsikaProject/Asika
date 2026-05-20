package handlers

import (
	"net/http"

	"asika/common/i18n"

	"github.com/gin-gonic/gin"
)

// GetI18n handles GET /api/v1/i18n?locale=xx
// Returns all translation key-value pairs for the requested locale as JSON.
func GetI18n(c *gin.Context) {
	locale := c.Query("locale")
	if locale == "" {
		locale = i18n.Locale()
	}
	msgs := i18n.AllMessages(locale)
	c.JSON(http.StatusOK, gin.H{
		"locale":   locale,
		"messages": msgs,
	})
}
