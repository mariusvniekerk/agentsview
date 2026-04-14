package main

import (
	"bytes"
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
