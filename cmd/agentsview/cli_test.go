package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestRootHelpListsAllCommands(t *testing.T) {
	cmd := newRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	help := out.String()
	for _, want := range []string{
		"serve",
		"sync",
		"prune",
		"update",
		"token-use",
		"import",
		"projects",
		"usage",
		"usage daily",
		"usage statusline",
		"version",
		"pg",
		"pg push",
		"pg status",
		"pg serve",
		"--host",
		"--port",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("help missing %q\n%s", want, help)
		}
	}
}

func TestRootHelpGroupsCompletionAtBottom(t *testing.T) {
	cmd := newRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	help := out.String()
	for _, want := range []string{
		"Core Commands:",
		"Data Commands:",
		"Usage Commands:",
		"Other Commands:",
		"completion             Generate the autocompletion script for the specified shell",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("help missing %q\n%s", want, help)
		}
	}

	core := strings.Index(help, "Core Commands:")
	data := strings.Index(help, "Data Commands:")
	usage := strings.Index(help, "Usage Commands:")
	other := strings.Index(help, "Other Commands:")
	completion := strings.Index(help, "completion             Generate the autocompletion script for the specified shell")
	serve := strings.Index(help, "serve                  Start server")
	usageCmd := strings.Index(help, "usage                  Token cost tracking and reporting")
	if !(core >= 0 && data > core && usage > data && other > usage) {
		t.Fatalf("unexpected group order\n%s", help)
	}
	if usageCmd < usage || usageCmd > other {
		t.Fatalf("usage commands not grouped separately\n%s", help)
	}
	if completion < other {
		t.Fatalf("completion not grouped at bottom\n%s", help)
	}
	if serve > other {
		t.Fatalf("serve command placed after other commands\n%s", help)
	}
}

func TestRootHelpDoesNotDuplicateCommandListings(t *testing.T) {
	cmd := newRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	help := out.String()
	if strings.Contains(help, "agentsview serve [flags]") {
		t.Fatalf("usage duplicates command listings\n%s", help)
	}
	for _, want := range []string{
		"Usage:\n  agentsview [flags]\n  agentsview <command>",
		"Core Commands:",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("help missing %q\n%s", want, help)
		}
	}
	if strings.Contains(help, "\nCommands:\n") {
		t.Fatalf("help should not include redundant Commands heading\n%s", help)
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

func TestRootHelpShowsOnlyTopLevelCompletionCommand(t *testing.T) {
	cmd := newRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	help := out.String()
	if !strings.Contains(help, "completion             Generate the autocompletion script for the specified shell") {
		t.Fatalf("root help missing top-level completion command\n%s", help)
	}
	for _, unwanted := range []string{
		"completion bash",
		"completion fish",
		"completion powershell",
		"completion zsh",
	} {
		if strings.Contains(help, unwanted) {
			t.Fatalf("root help should not list %q\n%s", unwanted, help)
		}
	}
}

func TestRootVersionFlag(t *testing.T) {
	cmd := newRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "agentsview ") {
		t.Fatalf("version output = %q", got)
	}
}
