package init

import "bufio"

func RunInteractiveWizard(reader *bufio.Reader) map[string]interface{} {
	cfg := make(map[string]interface{})

	StepHeader(1, "Operation Mode")
	mode := ConfigureMode(reader)
	cfg["mode"] = mode

	StepHeader(2, "Database Configuration")
	cfg["database"] = ConfigureDatabase(reader)

	StepHeader(3, "Platform Tokens")
	cfg["tokens"] = ConfigurePlatforms(reader)

	StepHeader(4, "Repository Group")
	cfg["repo_groups"] = ConfigureRepoGroup(reader, mode)

	StepHeader(5, "Notification Channels")
	cfg["notify"] = ConfigureNotifications(reader)

	StepHeader(6, "Admin Account & Server")
	serverMap, authMap := ConfigureServer(reader)
	cfg["server"] = serverMap
	cfg["auth"] = authMap

	StepHeader(7, "Self-Update Settings")
	cfg["updates"] = ConfigureUpdates(reader)

	return cfg
}
