package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestForwardArgsDropsLeadingSeparator(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"no separator", []string{"-p", "5010", "--experimental-https"}, []string{"-p", "5010", "--experimental-https"}},
		{"leading separator", []string{"--", "-p", "5010"}, []string{"-p", "5010"}},
		{"bare separator", []string{"--"}, nil},
		{"nil", nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := forwardArgs(c.in)
			if strings.Join(got, " ") != strings.Join(c.want, " ") {
				t.Errorf("forwardArgs(%v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

// TestToolchainCommandsDisableFlagParsing guards the passthrough contract: the
// script owns its flags, so the CLI must not parse them (TFC-007b).
func TestToolchainCommandsDisableFlagParsing(t *testing.T) {
	for _, cmd := range []*cobra.Command{devCmd, buildCmd, startCmd, checkCmd} {
		if !cmd.DisableFlagParsing {
			t.Errorf("%s: DisableFlagParsing = false, want true (flags forwarded to the script)", cmd.Name())
		}
		if cmd.Args == nil {
			t.Errorf("%s: Args = nil, want ArbitraryArgs", cmd.Name())
		}
	}
}
