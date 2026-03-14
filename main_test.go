package main

import (
	"strings"
	"testing"
)

func TestParseOptionsDebugUnsafeImpliesDebug(t *testing.T) {
	opts, err := parseOptions([]string{"--debug-unsafe"})
	if err != nil {
		t.Fatalf("parseOptions: %v", err)
	}
	if !opts.debug {
		t.Fatal("expected --debug-unsafe to enable debug logging")
	}
	if !opts.debugUnsafe {
		t.Fatal("expected debugUnsafe to be set")
	}
}

func TestParseOptionsHelpAndVersionAliases(t *testing.T) {
	opts, err := parseOptions([]string{"-h", "-v"})
	if err != nil {
		t.Fatalf("parseOptions: %v", err)
	}
	if !opts.showHelp {
		t.Fatal("expected -h to enable help")
	}
	if !opts.showVersion {
		t.Fatal("expected -v to enable version")
	}
}

func TestParseOptionsRejectsUnexpectedArgs(t *testing.T) {
	if _, err := parseOptions([]string{"unexpected"}); err == nil {
		t.Fatal("expected parseOptions to reject positional args")
	}
}

func TestHelpTextDocumentsSafeAndUnsafeDebug(t *testing.T) {
	help := helpText()
	if !strings.Contains(help, "--debug") {
		t.Fatal("expected help to mention --debug")
	}
	if !strings.Contains(help, "--debug-unsafe") {
		t.Fatal("expected help to mention --debug-unsafe")
	}
	if strings.Contains(help, "/tmp/atria-debug.log") {
		t.Fatal("expected help to not mention the old /tmp debug path")
	}
	if !strings.Contains(help, "debug.log") {
		t.Fatal("expected help to mention debug.log")
	}
	if !strings.Contains(help, "may capture secrets") {
		t.Fatal("expected help to warn about unsafe logging")
	}
}
