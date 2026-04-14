package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func executeCommand(root *cobra.Command, args ...string) (string, error) {
	_, output, err := executeCommandC(root, args...)
	return output, err
}

func executeCommandC(root *cobra.Command, args ...string) (*cobra.Command, string, error) {
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(args)

	cmd, err := root.ExecuteC()
	return cmd, buf.String(), err
}

func TestRootHelpShowsKeySectionsAndCommands(t *testing.T) {
	help, err := executeCommand(newRootCommand(), "--help")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{
		"Usage:\n  agentsview [flags]\n  agentsview <command> [flags]",
		"Core Commands:",
		"Data Commands:",
		"Usage Commands:",
		"Other Commands:",
		"serve                  Start server",
		"pg push                Push local data to PostgreSQL",
		"usage daily            Daily cost summary",
		"completion             Generate the autocompletion script for the specified shell",
		"Flags:",
		"--host string",
		"--port int",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("help missing %q\n%s", want, help)
		}
	}
}

func TestRootHelpKeepsSummaryClean(t *testing.T) {
	help, err := executeCommand(newRootCommand(), "--help")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, unwanted := range []string{
		"agentsview serve [flags]",
		"\nCommands:\n",
		"completion bash",
		"completion fish",
		"completion powershell",
		"completion zsh",
	} {
		if strings.Contains(help, unwanted) {
			t.Fatalf("root help should not include %q\n%s", unwanted, help)
		}
	}
}

func TestNormalizeFlagHelpWidth(t *testing.T) {
	tests := []struct {
		in   int
		want int
	}{
		{in: 0, want: 80},
		{in: -1, want: 80},
		{in: 79, want: 79},
		{in: 120, want: 120},
		{in: 160, want: 160},
		{in: 220, want: 160},
	}
	for _, tt := range tests {
		if got := normalizeFlagHelpWidth(tt.in); got != tt.want {
			t.Fatalf("normalizeFlagHelpWidth(%d) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestFlagHelpWidthFallback(t *testing.T) {
	if got := flagHelpWidth(&bytes.Buffer{}); got != 80 {
		t.Fatalf("flagHelpWidth(buffer) = %d, want 80", got)
	}

	f, err := os.CreateTemp(t.TempDir(), "help-width")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer f.Close()

	if got := flagHelpWidth(f); got != 80 {
		t.Fatalf("flagHelpWidth(file) = %d, want 80", got)
	}
}

func TestRootVersionFlag(t *testing.T) {
	got, err := executeCommand(newRootCommand(), "--version")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(got, "agentsview ") {
		t.Fatalf("version output = %q", got)
	}
}
