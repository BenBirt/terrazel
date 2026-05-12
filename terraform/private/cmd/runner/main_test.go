package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// --- Unit tests for checkVarFileDuplicates ---

func TestCheckVarFileDuplicates_NoFiles(t *testing.T) {
	dir := t.TempDir()
	writeJSON(t, filepath.Join(dir, "terrazel.auto.tfvars.json"), map[string]any{"region": "us-east-1"})
	if err := checkVarFileDuplicates(dir, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCheckVarFileDuplicates_NoOverlap(t *testing.T) {
	dir := t.TempDir()
	writeJSON(t, filepath.Join(dir, "terrazel.auto.tfvars.json"), map[string]any{"region": "us-east-1"})
	vf := filepath.Join(dir, "extra.tfvars.json")
	writeJSON(t, vf, map[string]any{"env": "prod"})
	if err := checkVarFileDuplicates(dir, []string{vf}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCheckVarFileDuplicates_VarsOverlap(t *testing.T) {
	dir := t.TempDir()
	writeJSON(t, filepath.Join(dir, "terrazel.auto.tfvars.json"), map[string]any{"region": "us-east-1"})
	vf := filepath.Join(dir, "extra.tfvars.json")
	writeJSON(t, vf, map[string]any{"region": "eu-west-1"})
	err := checkVarFileDuplicates(dir, []string{vf})
	if err == nil {
		t.Fatal("expected error for duplicate key, got nil")
	}
	if !strings.Contains(err.Error(), "region") {
		t.Errorf("expected error to mention the key name, got: %v", err)
	}
}

func TestCheckVarFileDuplicates_VarFilesOverlap(t *testing.T) {
	dir := t.TempDir()
	writeJSON(t, filepath.Join(dir, "terrazel.auto.tfvars.json"), map[string]any{"region": "us-east-1"})
	vf1 := filepath.Join(dir, "a.tfvars.json")
	vf2 := filepath.Join(dir, "b.tfvars.json")
	writeJSON(t, vf1, map[string]any{"env": "prod"})
	writeJSON(t, vf2, map[string]any{"env": "staging"})
	err := checkVarFileDuplicates(dir, []string{vf1, vf2})
	if err == nil {
		t.Fatal("expected error for duplicate key across var files, got nil")
	}
	if !strings.Contains(err.Error(), "env") {
		t.Errorf("expected error to mention the key name, got: %v", err)
	}
}

// --- Integration tests using the runner binary ---

// runnerBin returns the path to the compiled runner binary from Bazel runfiles,
// skipping the test if not running under Bazel.
func runnerBin(t *testing.T) string {
	t.Helper()
	srcdir := os.Getenv("TEST_SRCDIR")
	workspace := os.Getenv("TEST_WORKSPACE")
	if srcdir == "" || workspace == "" {
		t.Skip("not running under Bazel (TEST_SRCDIR/TEST_WORKSPACE not set)")
	}
	bin := filepath.Join(srcdir, workspace, "terraform/private/cmd/runner/runner_/runner")
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("runner binary not found at %s (check data dep): %v", bin, err)
	}
	return bin
}

func TestRunnerBin_DuplicateKeyError(t *testing.T) {
	bin := runnerBin(t)

	dir := t.TempDir()
	workTree := filepath.Join(dir, "work")
	const pkgDir = "mypkg"
	if err := os.MkdirAll(filepath.Join(workTree, pkgDir), 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(workTree, pkgDir, "terrazel.auto.tfvars.json"), map[string]any{"region": "us-east-1"})
	vf := filepath.Join(dir, "extra.tfvars.json")
	writeJSON(t, vf, map[string]any{"region": "eu-west-1"})

	cmd := exec.Command(bin,
		"--tofu=/usr/bin/false",
		"--work-tree="+workTree,
		"--package-dir="+pkgDir,
		"--state-dir="+filepath.Join(dir, "state"),
		"--command=plan",
		"--var-file="+vf,
	)
	cmd.Env = append(os.Environ(), "BUILD_WORKSPACE_DIRECTORY="+dir)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected runner to fail on duplicate key, but it succeeded")
	}
	if !strings.Contains(string(out), "region") {
		t.Errorf("expected output to mention the duplicate key, got: %s", out)
	}
}

func TestRunnerBin_Success(t *testing.T) {
	bin := runnerBin(t)

	dir := t.TempDir()

	// Write a minimal fake tofu binary that exits 0 for any invocation.
	fakeTofuSh := filepath.Join(dir, "tofu.sh")
	if err := os.WriteFile(fakeTofuSh, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	workTree := filepath.Join(dir, "work")
	const pkgDir = "mypkg"
	if err := os.MkdirAll(filepath.Join(workTree, pkgDir), 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(workTree, pkgDir, "terrazel.auto.tfvars.json"), map[string]any{"region": "us-east-1"})

	cmd := exec.Command(bin,
		"--tofu="+fakeTofuSh,
		"--work-tree="+workTree,
		"--package-dir="+pkgDir,
		"--state-dir="+filepath.Join(dir, "state"),
		"--command=plan",
	)
	cmd.Env = append(os.Environ(), "BUILD_WORKSPACE_DIRECTORY="+dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("runner failed unexpectedly: %v\n%s", err, out)
	}
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
