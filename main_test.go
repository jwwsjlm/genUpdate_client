package main

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

func TestApplyConfigUsesTokenFromEnvironment(t *testing.T) {
	resetFlagsForTest(t)
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{"url":"https://updates.example.com","name":"app","token":"config-token"}`), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}
	t.Setenv(tokenEnvName, "env-token")

	if err := applyConfig(configPath); err != nil {
		t.Fatalf("applyConfig returned error: %v", err)
	}
	if updateToken != "env-token" {
		t.Fatalf("updateToken = %q, want env-token", updateToken)
	}
}

func TestApplyConfigKeepsFlagTokenOverEnvironment(t *testing.T) {
	resetFlagsForTest(t)
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{"url":"https://updates.example.com","name":"app","token":"config-token"}`), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}
	t.Setenv(tokenEnvName, "env-token")
	if err := flag.CommandLine.Set("token", "flag-token"); err != nil {
		t.Fatalf("failed to set token flag: %v", err)
	}

	if err := applyConfig(configPath); err != nil {
		t.Fatalf("applyConfig returned error: %v", err)
	}
	if updateToken != "flag-token" {
		t.Fatalf("updateToken = %q, want flag-token", updateToken)
	}
}

func resetFlagsForTest(t *testing.T) {
	t.Helper()
	baseURL = ""
	appName = ""
	autoYes = false
	skipWait = false
	noProgress = false
	showVersion = false
	configPath = ""
	targetProcess = ""
	updateToken = ""
	processWaitTimeout = 0
	concurrency = 1

	oldCommandLine := flag.CommandLine
	flag.CommandLine = flag.NewFlagSet(t.Name(), flag.ContinueOnError)
	flag.StringVar(&baseURL, "url", "", "")
	flag.StringVar(&appName, "name", "", "")
	flag.BoolVar(&autoYes, "y", false, "")
	flag.BoolVar(&skipWait, "no-wait", false, "")
	flag.BoolVar(&noProgress, "no-progress", false, "")
	flag.StringVar(&updateToken, "token", "", "")
	flag.StringVar(&targetProcess, "process", "", "")
	flag.DurationVar(&processWaitTimeout, "wait-timeout", 0, "")
	flag.IntVar(&concurrency, "concurrency", 1, "")
	t.Cleanup(func() {
		flag.CommandLine = oldCommandLine
	})
}
