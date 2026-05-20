package init

import "bufio"

func ConfigureMode(reader *bufio.Reader) string {
	StepHeader(1, "Operation Mode")
	return Choose(reader, "Select operation mode", []string{"multi", "single"}, "multi")
}

func ConfigureDatabase(reader *bufio.Reader) map[string]string {
	StepHeader(2, "Database Configuration")
	dbType := Choose(reader, "Select database type", []string{"bbolt", "mongo"}, "bbolt")
	dbConfig := map[string]string{"type": dbType}
	if dbType == "mongo" {
		dbConfig["path"] = Prompt(reader, "MongoDB connection string", "mongodb://localhost:27017")
		dbConfig["name"] = Prompt(reader, "MongoDB database name", "asika")
	} else {
		dbConfig["path"] = Prompt(reader, "Database path", "/var/lib/asika/asika.db")
	}
	return dbConfig
}

func ConfigureServer(reader *bufio.Reader) (serverMap, authMap map[string]string) {
	StepHeader(6, "Server & Auth")
	listen := Prompt(reader, "Server listen address", ":8080")
	serverMode := Choose(reader, "Server mode", []string{"release", "debug"}, "release")
	jwtSecret := Prompt(reader, "JWT Secret (leave empty to auto-generate)", "")
	serverMap = map[string]string{
		"listen": listen,
		"mode":   serverMode,
	}
	authMap = map[string]string{
		"jwt_secret":   jwtSecret,
		"token_expiry": "72h",
	}
	return
}

func ConfigureUpdates(reader *bufio.Reader) map[string]interface{} {
	StepHeader(7, "Self-Update Settings")
	enableUpdates := Confirm(reader, "Enable automatic update check?")
	if enableUpdates {
		updateInterval := Prompt(reader, "Check interval", "24h")
		updateNotify := Confirm(reader, "Notify on new version?")
		return map[string]interface{}{
			"check":         true,
			"interval":      updateInterval,
			"notify_on_new": updateNotify,
		}
	}
	return map[string]interface{}{
		"check":         false,
		"interval":      "24h",
		"notify_on_new": false,
	}
}
