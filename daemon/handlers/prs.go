package handlers

import (
	"github.com/gin-gonic/gin"

	"asika/common/platforms"
	"asika/daemon/handlers/pr"
	"asika/daemon/handlers/webhook"
	"asika/daemon/polling"
	"asika/daemon/queue"
	"asika/daemon/syncer"
)

func GetClients() map[platforms.PlatformType]platforms.PlatformClient {
	return pr.GetClients()
}

func InitClients(c map[platforms.PlatformType]platforms.PlatformClient) {
	pr.InitClients(c)
	webhook.SetClients(c)
}

func InitSyncer(s *syncer.Syncer) {
	pr.InitSyncer(s)
}

func InitPoller(p *polling.Poller) {
	pr.InitPoller(p)
}

func InitQueueMgr(mgr *queue.Manager) {
	pr.InitQueueMgr(mgr)
}

var WebhookHandler = webhook.WebhookHandler

func ListPRs(c *gin.Context)       { pr.ListPRs(c) }
func GetPR(c *gin.Context)         { pr.GetPR(c) }
func ApprovePR(c *gin.Context)     { pr.ApprovePR(c) }
func ClosePR(c *gin.Context)       { pr.ClosePR(c) }
func ReopenPR(c *gin.Context)      { pr.ReopenPR(c) }
func MarkSpam(c *gin.Context)      { pr.MarkSpam(c) }
func CommentPR(c *gin.Context)     { pr.CommentPR(c) }
func BatchApprovePR(c *gin.Context) { pr.BatchApprovePR(c) }
func BatchClosePR(c *gin.Context)   { pr.BatchClosePR(c) }
func BatchLabelPR(c *gin.Context)   { pr.BatchLabelPR(c) }
func GetLogs(c *gin.Context)        { pr.GetLogs(c) }
func ExportLogs(c *gin.Context)     { pr.ExportLogs(c) }
