package server

import (
	"net/http"
	"net/http/pprof"

	"github.com/gin-gonic/gin"

	"asika/common/config"
	"asika/daemon/handlers"
	"asika/daemon/handlers/webhook"
	"asika/daemon/platform/feishu"
)

func (s *Server) setupRoutes() {
	s.engine.GET("/health", healthHandler)
	s.engine.GET("/metrics", metricsHandler)

	if s.cfg != nil && s.cfg.Server.EnablePprof {
		debug := s.engine.Group("/debug/pprof")
		debug.Use(RequireAuth())
		debug.Use(RequireRole("admin"))
		debug.GET("/", gin.WrapF(pprof.Index))
		debug.GET("/cmdline", gin.WrapF(pprof.Cmdline))
		debug.GET("/profile", gin.WrapF(pprof.Profile))
		debug.POST("/symbol", gin.WrapF(pprof.Symbol))
		debug.GET("/symbol", gin.WrapF(pprof.Symbol))
		debug.GET("/trace", gin.WrapF(pprof.Trace))
		debug.GET("/allocs", gin.WrapF(pprof.Handler("allocs").ServeHTTP))
		debug.GET("/block", gin.WrapF(pprof.Handler("block").ServeHTTP))
		debug.GET("/goroutine", gin.WrapF(pprof.Handler("goroutine").ServeHTTP))
		debug.GET("/heap", gin.WrapF(pprof.Handler("heap").ServeHTTP))
		debug.GET("/mutex", gin.WrapF(pprof.Handler("mutex").ServeHTTP))
		debug.GET("/threadcreate", gin.WrapF(pprof.Handler("threadcreate").ServeHTTP))
	}

	api := s.engine.Group("/api/v1")

	auth := api.Group("/auth")
	{
		auth.POST("/login", handlers.Login)
		auth.POST("/logout", handlers.Logout)
	}

	api.POST("/locale", handlers.SetLocale)
	api.GET("/i18n", handlers.GetI18n)

	wizard := api.Group("/wizard")
	{
		wizard.GET("", handlers.GetWizardSteps)
		wizard.POST("/step/complete", handlers.CompleteWizard)
		wizard.POST("/step/:step", handlers.SubmitWizardStep)
	}

	protected := api.Group("")
	protected.Use(RequireAuth())

	apiKeys := protected.Group("/apikeys")
	apiKeys.Use(RequireRole("admin"))
	{
		apiKeys.POST("", handlers.CreateAPIKey)
		apiKeys.GET("", handlers.ListAPIKeys)
		apiKeys.DELETE("/:id", handlers.RevokeAPIKey)
	}
	{
		users := protected.Group("/users")
		users.Use(RequireRole("admin"))
		{
			users.GET("", handlers.ListUsers)
			users.POST("", handlers.CreateUser)
			users.PUT("/:username", handlers.UpdateUser)
			users.DELETE("/:username", handlers.DeleteUser)
		}

		prs := protected.Group("/repos/:repo_group/prs")
		prs.Use(RequireAnyRole("viewer", "operator", "admin"))
		prs.Use(RequireRepoGroupAccess())
		prs.Use(RequireSpaceAccess())
		{
			prs.GET("", handlers.ListPRs)
			prs.POST("/sync", handlers.ListPRs)
			prs.GET("/:pr_id", handlers.GetPR)
			prs.GET("/:pr_id/diff", handlers.GetPRDiff)

			prsComment := prs.Group("")
			prsComment.Use(RequirePermission("comment"))
			{
				prsComment.POST("/:pr_id/comment", handlers.CommentPR)
				prsComment.POST("/:pr_id/comment-line", handlers.CommentPRLine)
			}

			prsApprove := prs.Group("")
			prsApprove.Use(RequirePermission("approve"))
			{
				prsApprove.POST("/:pr_id/approve", handlers.ApprovePR)
				prsApprove.POST("/batch/approve", handlers.BatchApprovePR)
			}

			prsClose := prs.Group("")
			prsClose.Use(RequirePermission("close"))
			{
				prsClose.POST("/:pr_id/close", handlers.ClosePR)
				prsClose.POST("/batch/close", handlers.BatchClosePR)
			}

			prsReopen := prs.Group("")
			prsReopen.Use(RequirePermission("reopen"))
			{
				prsReopen.POST("/:pr_id/reopen", handlers.ReopenPR)
			}

			prsSpam := prs.Group("")
			prsSpam.Use(RequirePermission("spam"))
			{
				prsSpam.POST("/:pr_id/spam", handlers.MarkSpam)
			}

			prsMerge := prs.Group("")
			prsMerge.Use(RequirePermission("merge"))
			{
				prsMerge.POST("/:pr_id/rebase", handlers.RebaseSinglePR)
				prsMerge.POST("/:pr_id/cherry-pick", handlers.CherryPickSinglePR)
				prsMerge.POST("/:pr_id/schedule-merge", handlers.ScheduleMerge)
				prsMerge.POST("/batch/rebase", handlers.BatchRebasePR)
			}

			prsRevert := prs.Group("")
			prsRevert.Use(RequirePermission("revert"))
			{
				prsRevert.POST("/:pr_id/revert", handlers.RevertPR)
			}

			prsLabel := prs.Group("")
			prsLabel.Use(RequirePermission("label"))
			{
				prsLabel.POST("/batch/label", handlers.BatchLabelPR)
			}

			prsAssign := prs.Group("")
			prsAssign.Use(RequirePermission("approve"))
			{
				prsAssign.POST("/:pr_id/assign", handlers.AssignReviewers)
				prsAssign.POST("/:pr_id/codeowners-assign", handlers.TriggerCodeOwnersAssign)
			}
		}

		queue := protected.Group("/queue/:repo_group")
		queue.Use(RequireAnyRole("viewer", "operator", "admin"))
		queue.Use(RequireRepoGroupAccess())
		{
			queue.GET("", handlers.GetQueue)

			queueWrite := queue.Group("")
			queueWrite.Use(RequirePermission("manage_queue"))
			{
				queueWrite.POST("/recheck", handlers.RecheckQueue)
				queueWrite.POST("/rebase", handlers.RebaseQueue)
				queueWrite.DELETE("", handlers.ClearQueue)
				queueWrite.DELETE("/:pr_id", handlers.RemoveFromQueue)
			}
		}

		logs := protected.Group("/logs")
		logs.Use(RequireAnyRole("viewer", "operator", "admin"))
		{
			logs.GET("", handlers.GetLogs)
			logs.GET("/export", handlers.ExportLogs)
		}

		cfgGroup := protected.Group("/config")
		cfgGroup.Use(RequireRole("admin"))
		{
			cfgGroup.GET("", handlers.GetConfig)
			cfgGroup.PUT("", handlers.UpdateConfig)
			cfgGroup.GET("/history", handlers.GetConfigHistory)
			cfgGroup.POST("/rollback", handlers.RollbackConfig)
		}

		protected.GET("/stats", handlers.GetStats)
		protected.GET("/stats/team", handlers.GetTeamStats)
		protected.GET("/reports", handlers.GetReportHistory)

		notifPrefs := protected.Group("/users/:username/notifications")
		{
			notifPrefs.GET("", handlers.GetNotificationPrefs)
			notifPrefs.PUT("", handlers.UpdateNotificationPrefs)
		}

		protected.GET("/audit", func(c *gin.Context) {
			user, _ := c.Get("username")
			c.HTML(http.StatusOK, "audit.html", gin.H{
				"title":    "Audit Logs - Asika",
				"username": user,
			})
		})

		protected.GET("/usage", handlers.GetUsage)

		sync := protected.Group("/sync")
		sync.Use(RequireAnyRole("viewer", "operator", "admin"))
		{
			sync.GET("/history", handlers.GetSyncHistory)
			sync.POST("/retry/:sync_id", handlers.RetrySync)
		}

		test := protected.Group("/test")
		test.Use(RequireRole("admin"))
		{
			test.POST("/notify", handlers.TestNotify)
		}

		protected.GET("/webhooks/health", webhook.WebhookHealthHandler)
		cfgGroup.POST("/dry-run", handlers.DryRunConfig)

		// Webhook configuration API
		webhooksAdmin := protected.Group("/webhooks")
		webhooksAdmin.Use(RequireRole("admin"))
		{
			webhooksAdmin.GET("", handlers.ListWebhooks)
			webhooksAdmin.POST("", handlers.CreateWebhook)
			webhooksAdmin.DELETE("/:index", handlers.DeleteWebhook)
			webhooksAdmin.POST("/:index/test", handlers.TestWebhook)
		}

		protected.GET("/feed.xml", handlers.GetFeed)
		feedAdmin := protected.Group("/feed")
		feedAdmin.Use(RequireRole("admin"))
		{
			feedAdmin.GET("/config", handlers.GetFeedConfig)
			feedAdmin.PUT("/config", handlers.UpdateFeedConfig)
		}

		admin := protected.Group("/admin")
		admin.Use(RequireRole("admin"))
		{
			admin.POST("/backup", handlers.CreateBackup)
			admin.GET("/backups", handlers.ListBackups)
			admin.POST("/restore", handlers.RestoreBackup)
		}

		protected.GET("/events", handlers.StreamEvents)

		protected.GET("/stats/bottlenecks", handlers.GetBottleneckStats)

		// Issue-PR links
		issueLinks := protected.Group("/repos/:repo_group")
		issueLinks.Use(RequireRepoGroupAccess())
		{
			issueLinks.GET("/issues/:issue_id/prs", handlers.GetIssueLinks)
			issueLinks.GET("/prs/:pr_id/issues", handlers.GetPRLinks)
			issueLinks.POST("/prs/:pr_id/sync-links", handlers.SyncIssueLinks)
		}

		// PR templates
		prTemplates := protected.Group("/repos/:repo_group")
		prTemplates.Use(RequireRepoGroupAccess())
		{
			prTemplates.GET("/template", handlers.GetPRTemplate)
			prTemplates.POST("/template/fetch", handlers.FetchTemplate)
			prTemplates.POST("/prs/:pr_id/checklist", handlers.CheckChecklist)
		}

		// PR dependencies
		prDeps := protected.Group("/repos/:repo_group")
		prDeps.Use(RequireRepoGroupAccess())
		{
			prDeps.GET("/prs/:pr_id/dependencies", handlers.GetPRDependencies)
			prDeps.GET("/prs/:pr_id/dependents", handlers.GetPRDependents)
			prDeps.POST("/prs/:pr_id/sync-deps", handlers.SyncDependencies)
		}

		// Cross-space dependencies
		crossSpaceDeps := protected.Group("/repos/:repo_group")
		crossSpaceDeps.Use(RequireRepoGroupAccess())
		{
			crossSpaceDeps.GET("/prs/:pr_id/cross-space-deps", handlers.ListCrossSpaceDeps)
		}
		crossSpaceWrite := protected.Group("/cross-space-deps")
		crossSpaceWrite.Use(RequirePermission("merge"))
		{
			crossSpaceWrite.GET("/:source_pr_id/:target_pr_id", handlers.GetCrossSpaceDeps)
			crossSpaceWrite.POST("/:source_pr_id/:target_pr_id/resolve", handlers.ResolveCrossSpaceDep)
		}

		// PR Stacks
		stacksGroup := protected.Group("/stacks")
		stacksGroup.Use(RequireAnyRole("viewer", "operator", "admin"))
		{
			stacksGroup.GET("", handlers.ListStacks)
			stacksGroup.GET("/:id", handlers.GetStack)

			stacksWrite := stacksGroup.Group("")
			stacksWrite.Use(RequirePermission("merge"))
			{
				stacksWrite.POST("", handlers.CreateStack)
				stacksWrite.DELETE("/:id", handlers.DeleteStack)
				stacksWrite.POST("/:id/members", handlers.AddStackMember)
				stacksWrite.DELETE("/:id/members/:pr_id", handlers.RemoveStackMember)
				stacksWrite.PUT("/:id/members/:pr_id/state", handlers.UpdateStackMemberState)
				stacksWrite.POST("/repos/:repo_group/prs/:pr_id/sync", handlers.SyncStackFromPR)
			}
		}
		protected.POST("/auth/temp-token", handlers.CreateTempToken)

		fp := protected.Group("/auth/fingerprints")
		{
			fp.POST("", handlers.RegisterFingerprint)
			fp.POST("/verify", handlers.VerifyFingerprintHandler)
			fp.GET("", handlers.ListFingerprints)
			fp.DELETE("/:id", handlers.RevokeFingerprint)
			fp.DELETE("", handlers.RevokeAllFingerprints)
		}

		spaces := protected.Group("/spaces")
		spaces.Use(RequireAnyRole("viewer", "operator", "admin"))
		{
			spaces.GET("", handlers.ListSpaces)
			spaces.GET("/:name", handlers.GetSpace)
			spaces.GET("/:name/members", handlers.GetSpaceMembers)

			spacesWrite := spaces.Group("")
			spacesWrite.Use(RequireRole("admin"))
			{
				spacesWrite.POST("", handlers.CreateSpace)
				spacesWrite.DELETE("/:name", handlers.DeleteSpace)
				spacesWrite.PUT("/:name/repo-groups", handlers.UpdateSpaceRepoGroups)
				spacesWrite.POST("/:name/members", handlers.AddSpaceMember)
				spacesWrite.DELETE("/:name/members/:username", handlers.RemoveSpaceMember)
			}
		}

		update := protected.Group("/self-update")
		update.Use(RequireRole("admin"))
		{
			update.GET("/check", handlers.CheckForUpdate)
			update.POST("/run", handlers.PerformWebUpdate)
		}

		staleGroup := protected.Group("/stale")
		staleGroup.Use(RequireRole("admin"))
		{
			staleGroup.POST("/check", handlers.HandleStaleCheck)
			staleGroup.POST("/check/:repo_group", handlers.HandleStaleCheck)
			staleGroup.POST("/unmark/:repo_group/:pr_number", handlers.HandleStaleUnmark)
		}
	}

	s.engine.GET("/", func(c *gin.Context) {
		if config.Current() == nil {
			c.Redirect(http.StatusFound, "/wizard")
			return
		}
		if cookie, err := c.Cookie("asika_token"); err == nil && cookie != "" {
			c.Redirect(http.StatusFound, "/dashboard")
		} else {
			c.Redirect(http.StatusFound, "/login")
		}
	})

	s.engine.GET("/wizard", func(c *gin.Context) {
		c.HTML(http.StatusOK, "wizard.html", gin.H{"title": "Setup Wizard - Asika"})
	})

	s.engine.GET("/login", func(c *gin.Context) {
		c.HTML(http.StatusOK, "login.html", gin.H{"title": "Login - Asika"})
	})

	ssr := s.engine.Group("")
	ssr.Use(SSRAuthRequired())
	{
		ssr.GET("/dashboard", func(c *gin.Context) {
			user := c.GetString("username")
			c.HTML(http.StatusOK, "dashboard.html", gin.H{
				"title":    "Dashboard - Asika",
				"username": user,
			})
		})

		ssr.GET("/prs", func(c *gin.Context) {
			user := c.GetString("username")
			c.HTML(http.StatusOK, "pr_list.html", gin.H{
				"title":    "PRs - Asika",
				"username": user,
			})
		})

		ssr.GET("/repos/:repo_group/prs/:pr_id", func(c *gin.Context) {
			user := c.GetString("username")
			c.HTML(http.StatusOK, "pr_detail.html", gin.H{
				"title":      "PR Detail - Asika",
				"username":   user,
				"repo_group": c.Param("repo_group"),
				"pr_id":      c.Param("pr_id"),
			})
		})

		ssr.GET("/queue", func(c *gin.Context) {
			user := c.GetString("username")
			c.HTML(http.StatusOK, "queue.html", gin.H{
				"title":    "Queue - Asika",
				"username": user,
			})
		})

		ssr.GET("/users", func(c *gin.Context) {
			user := c.GetString("username")
			c.HTML(http.StatusOK, "users.html", gin.H{
				"title":    "Users - Asika",
				"username": user,
			})
		})

		ssr.GET("/config", func(c *gin.Context) {
			user := c.GetString("username")
			c.HTML(http.StatusOK, "config.html", gin.H{
				"title":    "Config - Asika",
				"username": user,
			})
		})

		ssr.GET("/apikeys", func(c *gin.Context) {
			user := c.GetString("username")
			c.HTML(http.StatusOK, "apikeys.html", gin.H{
				"title":    "API Keys - Asika",
				"username": user,
			})
		})

		ssr.GET("/settings", func(c *gin.Context) {
			user := c.GetString("username")
			c.HTML(http.StatusOK, "settings.html", gin.H{
				"title":    "Settings - Asika",
				"username": user,
			})
		})

		ssr.GET("/usage", func(c *gin.Context) {
			user := c.GetString("username")
			c.HTML(http.StatusOK, "usage.html", gin.H{
				"title":    "Usage - Asika",
				"username": user,
			})
		})

		ssr.GET("/spaces", func(c *gin.Context) {
			user := c.GetString("username")
			c.HTML(http.StatusOK, "spaces.html", gin.H{
				"title":    "Team Spaces - Asika",
				"username": user,
			})
		})

		ssr.GET("/team", func(c *gin.Context) {
			user := c.GetString("username")
			c.HTML(http.StatusOK, "team.html", gin.H{
				"title":    "Team Stats - Asika",
				"username": user,
			})
		})

		ssr.GET("/notifications", func(c *gin.Context) {
			user := c.GetString("username")
			c.HTML(http.StatusOK, "notifications.html", gin.H{
				"title":    "Notification Preferences - Asika",
				"username": user,
			})
		})

		ssr.GET("/reports", func(c *gin.Context) {
			user := c.GetString("username")
			c.HTML(http.StatusOK, "reports.html", gin.H{
				"title":    "Reports - Asika",
				"username": user,
			})
		})
	}

	s.engine.GET("/manifest.json", manifestHandler)
	s.engine.GET("/sw.js", serviceWorkerHandler)

	s.engine.POST("/webhook/:repo_group/:platform", webhook.WebhookHandler)
	s.engine.POST("/api/v1/feishu/event", feishu.FeishuEventHandler)
}
