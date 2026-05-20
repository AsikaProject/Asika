package init

import "bufio"

func RunInteractiveWizard(reader *bufio.Reader) map[string]interface{} {
	cfg := make(map[string]interface{})

	mode := ConfigureMode(reader)
	cfg["mode"] = mode

	cfg["database"] = ConfigureDatabase(reader)

	StepHeader(3, "Platform Tokens")
	cfg["tokens"] = ConfigurePlatforms(reader)

	StepHeader(4, "Repository Group")
	cfg["repo_groups"] = ConfigureRepoGroup(reader, mode)

	StepHeader(5, "Notification Channels")
	cfg["notify"] = ConfigureNotifications(reader)

	serverMap, authMap := ConfigureServer(reader)
	cfg["server"] = serverMap
	cfg["auth"] = authMap

	cfg["updates"] = ConfigureUpdates(reader)

	return cfg
}
