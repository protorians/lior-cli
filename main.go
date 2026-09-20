package main

import (
	_ "embed"
	"encoding/json"

	"github.com/protorians/lior-cli/cmd"
)

// Build info. The placeholders keep a plain `go run` / `go build` usable;
// the compiled-in `app.config.json` aligns version, branch, commit and date
// with the workspace (single source of truth). Release tooling overrides them
// via ldflags (see .goreleaser.yaml, scripts/dev-install.sh).
var (
	version = "dev"
	branch  = "none"
	commit  = "none"
	date    = "unknown"
)

// appConfig is the workspace `app.config.json` registry baked into the
// binary. It parameterises the CLI (API base URLs and timeouts) and carries
// the build info (version/branch/commit/date); a local `app.config.json`
// overrides the API registry at call time.
//
//go:embed app.config.json
var appConfig []byte

// buildInfo aligns version/branch/commit/date with the embedded
// `app.config.json`. ldflags-injected values win (they differ from the
// placeholders), so release builds keep reporting the exact tag/build.
func buildInfo() (string, string, string, string) {
	var cfg struct {
		Version string `json:"version"`
		Branch  string `json:"branch"`
		Commit  string `json:"commit"`
		Date    string `json:"date"`
	}
	if err := json.Unmarshal(appConfig, &cfg); err == nil {
		if cfg.Version != "" && version == "dev" {
			version = cfg.Version
		}
		if cfg.Branch != "" && branch == "none" {
			branch = cfg.Branch
		}
		if cfg.Commit != "" && commit == "none" {
			commit = cfg.Commit
		}
		if cfg.Date != "" && date == "unknown" {
			date = cfg.Date
		}
	}
	return version, branch, commit, date
}

func main() {
	version, branch, commit, date := buildInfo()
	cmd.Execute(version, branch, commit, date, appConfig)
}
