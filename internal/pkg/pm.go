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

// DevDependencyArgs returns the package-manager-specific arguments to add
// packages as dev dependencies (installed within the package manager's scope).
// It returns nil for an unsupported package manager.
func DevDependencyArgs(pm string, packages ...string) []string {
	var flag []string
	switch pm {
	case "bun":
		flag = []string{"add", "-d"}
	case "pnpm":
		flag = []string{"add", "-D"}
	case "yarn":
		flag = []string{"add", "-D"}
	case "npm":
		flag = []string{"install", "-D"}
	default:
		return nil
	}
	return append(flag, packages...)
}

// DependencyArgs returns the package-manager-specific arguments to add runtime
// dependencies. It returns nil for an unsupported package manager.
func DependencyArgs(pm string, packages ...string) []string {
	var flag []string
	switch pm {
	case "bun", "pnpm", "yarn":
		flag = []string{"add"}
	case "npm":
		flag = []string{"install"}
	default:
		return nil
	}
	return append(flag, packages...)
}
