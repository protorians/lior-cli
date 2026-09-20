package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/protorians/lior-cli/internal/audit"
	"github.com/protorians/lior-cli/internal/config"
	"github.com/protorians/lior-cli/internal/debug"
	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/moduletest"
	"github.com/protorians/lior-cli/internal/pkg"
	"github.com/protorians/lior-cli/internal/toolchain"
	"github.com/protorians/lior-cli/internal/tui"
)

// Gate check identities.
const (
	gateDebug = "debug"
	gateTest  = "test"
	gateAudit = "audit"
)

// gateChecks maps an application lifecycle command to the module health checks
// required before it is proxied (spec docs/specs/liorian-toolchain.md, §5.7):
// the modules must be healthy before the application starts or is built.
//   - dev   → debug + test
//   - build → debug + test + audit
//   - start → debug + test + audit
var gateChecks = map[string][]string{
	toolchain.Dev:   {gateDebug, gateTest},
	toolchain.Build: {gateDebug, gateTest, gateAudit},
	toolchain.Start: {gateDebug, gateTest, gateAudit},
}

// runToolchainGate runs the module health checks required before an
// application lifecycle command is proxied. It returns nil when the command
// requires no check, or when the project has no module to check; otherwise it
// runs the checks in order and aborts on the first failing one.
func runToolchainGate(root string, cfg config.Config, name string) error {
	checks, ok := gateChecks[name]
	if !ok || len(checks) == 0 {
		return nil
	}
	if !hasModules(root) {
		return nil
	}
	for _, check := range checks {
		if err := runGateCheck(root, cfg, check); err != nil {
			return err
		}
	}
	return nil
}

// hasModules reports whether the project has at least one module to check.
func hasModules(root string) bool {
	dir := filepath.Join(root, config.ExternalModulesDir)
	if !pkg.DirExists(dir) {
		return false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() && pkg.FileExists(filepath.Join(dir, e.Name(), config.ManifestFileName)) {
			return true
		}
	}
	return false
}

// gateRunResult is the outcome of a single module health check, kept so the
// gate can render the failing details after the live step view closed.
type gateRunResult struct {
	failures int
	debug    []*debug.DebugResult
	test     []*moduletest.TestResult
	audit    *audit.AuditResult
}

// runGateCheck runs a single module health check under the live step view and
// returns a categorized error when the check fails.
func runGateCheck(root string, cfg config.Config, check string) error {
	res, err := tui.RunWithSteps(i18n.T("gate.spinner."+check), func(ctx context.Context, report func(tui.Step)) (gateRunResult, error) {
		return runGateCheckInner(ctx, root, cfg, check, report)
	})
	if errors.Is(err, tui.ErrCancelled) {
		return pkg.NewError(i18n.T("cat.toolchain"), i18n.T("gate.cancelled"), pkg.ExitCancelled)
	}
	if err != nil {
		if res.failures > 0 {
			printGateDetails(check, res)
		}
		return err
	}
	return nil
}

// runGateCheckInner drives a single module health check, forwarding every step
// live. On success it returns a zero-failure result; on failure it returns the
// number of failing modules/errors and a categorized error.
func runGateCheckInner(ctx context.Context, root string, cfg config.Config, check string, report func(tui.Step)) (gateRunResult, error) {
	switch check {
	case gateDebug:
		results, err := (&debug.Debugger{Root: root, Reporter: report}).DebugAllCtx(ctx)
		if err != nil {
			return gateRunResult{}, err
		}
		res := gateRunResult{debug: results}
		for _, r := range results {
			if r.Status == "ERROR" {
				res.failures++
			}
		}
		if res.failures == 0 {
			return res, nil
		}
		return res, pkg.NewErrorWithFix(
			i18n.T("cat.toolchain"),
			i18n.Tf("gate.failed.debug", res.failures),
			i18n.Tf("gate.failed.fix", "debug"),
			pkg.ExitBuild,
		)

	case gateTest:
		tester := &moduletest.Tester{
			Root:                  root,
			Config:                cfg.Test,
			ProjectPackageManager: cfg.Project.PackageManager,
			Reporter:              report,
		}
		results, err := tester.TestAllCtx(ctx)
		if err != nil {
			return gateRunResult{}, err
		}
		res := gateRunResult{test: results}
		for _, r := range results {
			if r.Status == "ERROR" {
				res.failures++
			}
		}
		if res.failures == 0 {
			return res, nil
		}
		return res, pkg.NewErrorWithFix(
			i18n.T("cat.toolchain"),
			i18n.Tf("gate.failed.test", res.failures),
			i18n.Tf("gate.failed.fix", "test"),
			pkg.ExitTest,
		)

	case gateAudit:
		result, err := (&audit.Auditor{Root: root}).AuditModules("")
		if err != nil {
			return gateRunResult{}, err
		}
		res := gateRunResult{audit: result, failures: result.TotalErrors()}
		if res.failures == 0 {
			return res, nil
		}
		return res, pkg.NewErrorWithFix(
			i18n.T("cat.toolchain"),
			i18n.Tf("gate.failed.audit", res.failures),
			i18n.Tf("gate.failed.fix", "audit"),
			pkg.ExitError,
		)
	}
	return gateRunResult{}, nil
}

// printGateDetails renders the failing module details of a health check so the
// developer sees what blocked the command.
func printGateDetails(check string, res gateRunResult) {
	s := tui.NewStyles()
	switch check {
	case gateDebug:
		for _, r := range res.debug {
			if r.Status != "ERROR" || len(r.Logs) == 0 {
				continue
			}
			logs := debug.FormatDebugLogs(r.Module, r.Logs)
			fmt.Println(s.LogsBlock(i18n.Tf("debug.logs_for", r.Module), logs))
			fmt.Println()
		}
	case gateTest:
		for _, r := range res.test {
			if r.Status != "ERROR" || len(r.Logs) == 0 {
				continue
			}
			logs := moduletest.FormatTestLogs(r.Module, r.Logs)
			fmt.Println(s.LogsBlock(i18n.Tf("test.logs_for", r.Module), logs))
			fmt.Println()
		}
	case gateAudit:
		if res.audit != nil {
			printAuditResult(res.audit)
		}
	}
}
