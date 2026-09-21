package cmd

import "testing"

// TestRepairNoInteractionFlagRegistered verifies the --no-interaction flag is
// exposed by the repair command.
func TestRepairNoInteractionFlagRegistered(t *testing.T) {
	if repairCmd.Flags().Lookup("no-interaction") == nil {
		t.Fatal("le flag --no-interaction doit être enregistré sur la commande repair")
	}
}

// TestRepairRenamePromptNonInteractive verifies the rename prompt declines in a
// non-interactive context (no TTY), so the rename is reported as a manual
// instruction instead of blocking on input.
func TestRepairRenamePromptNonInteractive(t *testing.T) {
	value, ok := repairRenamePrompt("rename?", "com.example.mod")
	if ok {
		t.Errorf("repairRenamePrompt hors TTY = accepted, want declined")
	}
	if value != "" {
		t.Errorf("repairRenamePrompt hors TTY = %q, want empty", value)
	}
}
