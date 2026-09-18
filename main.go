package main

import (
	_ "embed"

	"github.com/protorians/liorian-cli/cmd"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// appConfig is the workspace `app.config.json` registry baked into the
// binary. It parameterises the CLI (API base URLs and timeouts) so commands
// keep working outside a workspace; a local `app.config.json` overrides it.
//
//go:embed app.config.json
var appConfig []byte

func main() {
	cmd.Execute(version, commit, date, appConfig)
}
