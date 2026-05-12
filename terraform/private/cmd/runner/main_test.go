package main_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

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

// setup creates a work tree with the given vars written to terrazel.auto.tfvars.json,
// and a fake tofu binary that exits 0 for any invocation. Returns the runner
// command pre-loaded with all required flags; callers may append --var-file flags.
func setup(t *testing.T, vars map[string]any) (cmd *exec.Cmd, addVarFile func(map[string]any) string) {
	t.Helper()
	bin := runnerBin(t)
	dir := t.TempDir()

	fakeTofuSh := filepath.Join(dir, "tofu.sh")
	if err := os.WriteFile(fakeTofuSh, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	workTree := filepath.Join(dir, "work")
	const pkgDir = "mypkg"
	if err := os.MkdirAll(filepath.Join(workTree, pkgDir), 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(workTree, pkgDir, "terrazel.auto.tfvars.json"), vars)

	var varFileCounter int
	addVarFile = func(contents map[string]any) string {
		varFileCounter++
		path := filepath.Join(dir, fmt.Sprintf("varfile%d.tfvars.json", varFileCounter))
		writeJSON(t, path, contents)
		return path
	}

	cmd = exec.Command(bin,
		"--tofu="+fakeTofuSh,
		"--work-tree="+workTree,
		"--package-dir="+pkgDir,
		"--state-dir="+filepath.Join(dir, "state"),
		"--command=plan",
	)
	cmd.Env = append(os.Environ(), "BUILD_WORKSPACE_DIRECTORY="+dir)
	return cmd, addVarFile
}

func TestRunner_NoVarFiles(t *testing.T) {
	cmd, _ := setup(t, map[string]any{"region": "us-east-1"})
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("runner failed unexpectedly: %v\n%s", err, out)
	}
}

func TestRunner_VarFileNoOverlap(t *testing.T) {
	cmd, addVarFile := setup(t, map[string]any{"region": "us-east-1"})
	cmd.Args = append(cmd.Args, "--var-file="+addVarFile(map[string]any{"env": "prod"}))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("runner failed unexpectedly: %v\n%s", err, out)
	}
}

func TestRunner_VarFileOverlapsVars(t *testing.T) {
	cmd, addVarFile := setup(t, map[string]any{"region": "us-east-1"})
	cmd.Args = append(cmd.Args, "--var-file="+addVarFile(map[string]any{"region": "eu-west-1"}))
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected runner to fail on duplicate key, but it succeeded")
	}
	if !strings.Contains(string(out), "region") {
		t.Errorf("expected output to mention the duplicate key, got: %s", out)
	}
}

func TestRunner_VarFilesOverlapEachOther(t *testing.T) {
	cmd, addVarFile := setup(t, map[string]any{"region": "us-east-1"})
	cmd.Args = append(cmd.Args,
		"--var-file="+addVarFile(map[string]any{"env": "prod"}),
		"--var-file="+addVarFile(map[string]any{"env": "staging"}),
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected runner to fail on duplicate key across var files, but it succeeded")
	}
	if !strings.Contains(string(out), "env") {
		t.Errorf("expected output to mention the duplicate key, got: %s", out)
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
