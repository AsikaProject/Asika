package version

var (
	Version = "dev"   // overridden by -ldflags at build time; format: YYYYMMDD[SUB]-HASH
	Enabled = "false" // set via ldflags for Linux standalone binaries
)
