package pkg

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// BumpPatch increments the patch component of a SemVer string:
// `1.2.3` → `1.2.4`. Pre-release/build metadata is dropped. Returns an error
// for malformed versions.
func BumpPatch(version string) (string, error) {
	v := strings.TrimSpace(version)
	v = strings.TrimPrefix(v, "v")
	if !semverStrictRE.MatchString(v) {
		return "", fmt.Errorf("version %q invalid (SemVer expected)", version)
	}

	// Strip pre-release and build metadata.
	if idx := strings.IndexAny(v, "-+"); idx != -1 {
		v = v[:idx]
	}

	segments := strings.SplitN(v, ".", 3)
	if len(segments) != 3 {
		return "", fmt.Errorf("version %q invalid (SemVer expected)", version)
	}
	patch, err := strconv.Atoi(segments[2])
	if err != nil {
		return "", fmt.Errorf("version %q invalid (SemVer expected)", version)
	}
	return fmt.Sprintf("%s.%s.%d", segments[0], segments[1], patch+1), nil
}

// semverStrictRE matches a strict SemVer 2.0.0 version (without a leading `v`).
var semverStrictRE = regexp.MustCompile(`^(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
