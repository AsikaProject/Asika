package version

var (
	// Version is overridden by -ldflags at build time; format: YYYYMMDD[SUB][-HASH]
	Version = "dev"
	// Enabled is set via ldflags for Linux standalone binaries.
	Enabled = "false"
	// Channel identifies the distribution channel and controls whether the
	// self-update command is available. Overridden by -ldflags.
	//   "release" — GitHub release tarball, built via build.sh build or the
	//               release workflow's build job. Self-update is allowed.
	//   "package" — deb / pkg / inno setup / docker image. Self-update is
	//               blocked; users upgrade via their package manager or by
	//               pulling a new container image.
	//   "manual"  — local `go build` without ldflags. Self-update is blocked;
	//               users reinstall via build.sh or download a release.
	Channel = "manual"
)
