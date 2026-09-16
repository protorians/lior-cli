package pkg

// PackageManagers is the detection + install order of the supported package
// managers (spec FR-001: bun → pnpm → yarn → npm).
var PackageManagers = []string{"bun", "pnpm", "yarn", "npm"}

// DetectPackageManager returns the name of the first package manager
// available on the PATH (bun → pnpm → yarn → npm), or "" when none is
// installed.
func DetectPackageManager() string {
	for _, pm := range PackageManagers {
		if HasCommand(pm) {
			return pm
		}
	}
	return ""
}
