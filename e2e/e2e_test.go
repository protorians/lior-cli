// Package e2e runs the end-to-end (testscript) suite of the Lior CLI
// against a mock liorian-connect API (spec §12: TC-001 → TC-029).
package e2e

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/protorians/lior-cli/e2e/mockapi"
	"github.com/rogpeppe/go-internal/testscript"
)

// liorianBin is the path of the CLI binary built in TestMain.
var liorianBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "liorian-e2e-")
	if err != nil {
		panic(err)
	}
	liorianBin = filepath.Join(dir, "liorian")

	// Build the CLI from the repository root (the test binary runs with cwd
	// set to this package's directory).
	root, err := filepath.Abs("..")
	if err != nil {
		panic(err)
	}
	build := exec.Command("go", "build", "-o", liorianBin, ".")
	build.Dir = root
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		panic("échec du build de la CLI pour l'E2E : " + err.Error())
	}

	os.Exit(m.Run())
}

func TestScripts(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir: "testdata/scripts",
		Setup: func(env *testscript.Env) error {
			// Whole CLI under test is the freshly built binary.
			env.Setenv("LIORIAN", liorianBin)
			// Network: a dedicated mock liorian-auth API per script, so
			// store/auth state never leaks between TC scenarios. The mock
			// also serves the public catalog (`/api/catalog/*`), which the
			// marketplace commands reach through LIORIAN_STORE_API.
			server := httptest.NewServer(mockapi.New().Handler())
			env.Setenv("LIORIAN_AUTH_API", server.URL)
			env.Setenv("LIORIAN_STORE_API", server.URL)
			// Deterministic, isolated state: force the encrypted-file vault
			// and skip the update check.
			env.Setenv("LIORIAN_CLI_STORE", "file")
			env.Setenv("LIORIAN_CLI_SKIP_UPDATE", "1")
			// Portable fixture toolchain (fake bun/npm/tsc/node) used by
			// init/install, debug and type-check scripts. PATH is deliberately
			// hermetic: the fixture binaries first, then only the standard
			// system directories. The developer machine's own PATH is NOT
			// inherited, so globally installed tools (esbuild, tsup, tsc,
			// vitest…) can never leak into the scenarios and skew the
			// bundler/type-check resolution (spec §5.9).
			env.Setenv("PATH", fixturesBin()+string(os.PathListSeparator)+"/usr/bin"+string(os.PathListSeparator)+"/bin")
			// testscript defaults HOME to the read-only `/no-home`: give each
			// script a writable, isolated home so the vault fallbacks
			// (credentials.enc / signing.enc / machine.secret) are deterministic.
			home := filepath.Join(env.WorkDir, "home")
			if err := os.MkdirAll(home, 0o755); err != nil {
				return err
			}
			env.Setenv("HOME", home)
			return nil
		},
	})
}

// fixturesBin returns the directory containing the fake package-manager and
// TypeScript toolchain executables used by the scripts.
func fixturesBin() string {
	dir, err := filepath.Abs(filepath.Join("testdata", "fixtures", "bin"))
	if err != nil {
		panic(err)
	}
	return dir
}
