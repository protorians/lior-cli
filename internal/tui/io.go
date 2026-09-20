package tui

import (
	"errors"
	"os"

	"github.com/jetbrains/lior-cli/internal/i18n"
	"golang.org/x/term"
)

// IsInteractive reports whether stdin and stdout are terminals, i.e. whether
// Bubble Tea prompts and spinners can run.
func IsInteractive() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

// RequireInteractive returns an explicit error when the environment is not a
// terminal (helpful in CI or piped contexts).
func RequireInteractive(what string) error {
	return errors.New(i18n.Tf("tui.error.interactive", what))
}
