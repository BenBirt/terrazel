package main_test

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var runnerBinPath = flag.String("runner", "", "path to the runner binary under test")

func runnerBin(t *testing.T) string {
	t.Helper()
	if *runnerBinPath == "" {
		t.Skip("runner binary path not set; pass -runner=<path> or run via bazel test")
	}
	if filepath.IsAbs(*runnerBinPath) {
		return *runnerBinPath
	}
	// Under bazel test, the flag is an rlocation path; resolve it against the
	// runfiles directory so exec.Command receives an absolute path.
	if dir := os.Getenv("RUNFILES_DIR"); dir != "" {
		return filepath.Join(dir, *runnerBinPath)
	}
	if dir := os.Getenv("TEST_SRCDIR"); dir != "" {
		return filepath.Join(dir, *runnerBinPath)
	}
	return *runnerBinPath
}

// invocation records a single call to the fake tofu binary.
type invocation struct {
	cwd  string
	args []string
}

func (inv invocation) hasArg(arg string) bool {
	for _, a := range inv.args {
		if a == arg {
			return true
		}
	}
	return false
}

func (inv invocation) hasArgWithPrefix(prefix string) bool {
	for _, a := range inv.args {
		if strings.HasPrefix(a, prefix) {
			return true
		}
	}
	return false
}

// parseInvocations reads the structured log written by the fake tofu script.
// Each invocation is a block of lines:
//
//	cwd=<working directory>
//	arg=<arg1>
//	arg=<arg2>
//	---
func parseInvocations(data []byte) []invocation {
	var invs []invocation
	var cur *invocation
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case line == "---":
			if cur != nil {
				invs = append(invs, *cur)
				cur = nil
			}
		case strings.HasPrefix(line, "cwd="):
			if cur == nil {
				cur = &invocation{}
			}
			cur.cwd = strings.TrimPrefix(line, "cwd=")
		case strings.HasPrefix(line, "arg="):
			if cur == nil {
				cur = &invocation{}
			}
			cur.args = append(cur.args, strings.TrimPrefix(line, "arg="))
		}
	}
	return invs
}

// setup creates a temporary work tree with the given vars written to
// terrazel.auto.tfvars.json, and a fake tofu script that logs each invocation
// to a temp file. It returns:
//   - a pre-configured runner Cmd (callers may append --var-file flags before running)
//   - addVarFile: writes a .tfvars.json file and returns its path
//   - invocations: reads and returns all recorded tofu invocations
func setup(t *testing.T, vars map[string]any) (
	cmd *exec.Cmd,
	addVarFile func(map[string]any) string,
	invocations func() []invocation,
) {
	t.Helper()
	bin := runnerBin(t)
	dir := t.TempDir()

	logPath := filepath.Join(dir, "tofu-invocations.log")
	fakeTofuSh := filepath.Join(dir, "tofu.sh")
	script := "#!/bin/sh\n" +
		"{ printf 'cwd=%s\\n' \"$(pwd)\"; for a in \"$@\"; do printf 'arg=%s\\n' \"$a\"; done; printf -- '---\\n'; } >> \"$TOFU_LOG\"\n" +
		"exit 0\n"
	if err := os.WriteFile(fakeTofuSh, []byte(script), 0o755); err != nil {
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

	invocations = func() []invocation {
		t.Helper()
		data, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatalf("read tofu invocation log: %v", err)
		}
		return parseInvocations(data)
	}

	cmd = exec.Command(bin,
		"--tofu="+fakeTofuSh,
		"--work-tree="+workTree,
		"--package-dir="+pkgDir,
		"--state-dir="+filepath.Join(dir, "state"),
		"--command=plan",
	)
	cmd.Env = append(os.Environ(),
		"BUILD_WORKSPACE_DIRECTORY="+dir,
		"TOFU_LOG="+logPath,
	)
	return cmd, addVarFile, invocations
}

func TestRunner_NoVarFiles(t *testing.T) {
	cmd, _, invocations := setup(t, map[string]any{"region": "us-east-1"})
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("runner failed unexpectedly: %v\n%s", err, out)
	}
	invs := invocations()
	// Expect: init, plan.
	if len(invs) != 2 {
		t.Fatalf("expected 2 tofu invocations (init, plan), got %d: %+v", len(invs), invs)
	}
	if !invs[0].hasArg("init") {
		t.Errorf("first invocation should be 'init', got args: %v", invs[0].args)
	}
	plan := invs[1]
	if !plan.hasArg("plan") {
		t.Errorf("second invocation should be 'plan', got args: %v", plan.args)
	}
	if !plan.hasArg("-input=false") {
		t.Errorf("plan invocation missing -input=false, got args: %v", plan.args)
	}
	if plan.hasArgWithPrefix("-var-file=") {
		t.Errorf("plan invocation should have no -var-file, got args: %v", plan.args)
	}
}

func TestRunner_VarFileNoOverlap(t *testing.T) {
	cmd, addVarFile, invocations := setup(t, map[string]any{"region": "us-east-1"})
	vf := addVarFile(map[string]any{"env": "prod"})
	cmd.Args = append(cmd.Args, "--var-file="+vf)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("runner failed unexpectedly: %v\n%s", err, out)
	}
	invs := invocations()
	if len(invs) != 2 {
		t.Fatalf("expected 2 tofu invocations (init, plan), got %d: %+v", len(invs), invs)
	}
	plan := invs[1]
	if !plan.hasArg("-var-file=" + vf) {
		t.Errorf("plan invocation missing -var-file=%s, got args: %v", vf, plan.args)
	}
	if !plan.hasArg("-input=false") {
		t.Errorf("plan invocation missing -input=false, got args: %v", plan.args)
	}
}

func TestRunner_VarFileOverlapsVars(t *testing.T) {
	cmd, addVarFile, _ := setup(t, map[string]any{"region": "us-east-1"})
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
	cmd, addVarFile, _ := setup(t, map[string]any{"region": "us-east-1"})
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

// TestRunner_WithDataFile verifies that a work tree containing arbitrary data
// files (e.g. placed there by the `data` attribute) does not interfere with
// normal runner execution. The files are simply present on disk so that
// Terraform's file() function can read them.
func TestRunner_WithDataFile(t *testing.T) {
	cmd, _, invocations := setup(t, map[string]any{"region": "us-east-1"})

	// Simulate a data file materialized by the `data` attribute — its location
	// is determined at analysis time by the Bazel rule (short_path under the
	// work tree), so here we just place it inside the work tree directory.
	workTree := ""
	for _, arg := range cmd.Args {
		if strings.HasPrefix(arg, "--work-tree=") {
			workTree = strings.TrimPrefix(arg, "--work-tree=")
		}
	}
	if workTree == "" {
		t.Fatal("could not find --work-tree flag in runner cmd args")
	}
	dataPath := filepath.Join(workTree, "mypkg", "config.json")
	if err := os.WriteFile(dataPath, []byte(`{"key":"value"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("runner failed with data file present: %v\n%s", err, out)
	}
	invs := invocations()
	if len(invs) != 2 {
		t.Fatalf("expected 2 tofu invocations (init, plan), got %d: %+v", len(invs), invs)
	}
	if !invs[0].hasArg("init") {
		t.Errorf("first invocation should be 'init', got args: %v", invs[0].args)
	}
	if !invs[1].hasArg("plan") {
		t.Errorf("second invocation should be 'plan', got args: %v", invs[1].args)
	}
}

func TestRunner_ExtraArgsPassedToPlan(t *testing.T) {
	cmd, _, invocations := setup(t, map[string]any{"region": "us-east-1"})
	cmd.Args = append(cmd.Args, "--", "--target=aws_instance.foo")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("runner failed unexpectedly: %v\n%s", err, out)
	}
	invs := invocations()
	if len(invs) != 2 {
		t.Fatalf("expected 2 tofu invocations (init, plan), got %d: %+v", len(invs), invs)
	}
	plan := invs[1]
	if !plan.hasArg("--target=aws_instance.foo") {
		t.Errorf("plan invocation missing extra arg, got args: %v", plan.args)
	}
}

func TestRunner_ExtraArgsPassedToApply(t *testing.T) {
	cmd, _, invocations := setup(t, map[string]any{"region": "us-east-1"})
	cmd.Args = append(cmd.Args,
		"--command=apply",
		"--", "--replace=aws_instance.foo",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("runner failed unexpectedly: %v\n%s", err, out)
	}
	invs := invocations()
	// Expect: init, apply.
	if len(invs) != 2 {
		t.Fatalf("expected 2 tofu invocations (init, apply), got %d: %+v", len(invs), invs)
	}
	apply := invs[1]
	if !apply.hasArg("apply") {
		t.Errorf("second invocation should be 'apply', got args: %v", apply.args)
	}
	if !apply.hasArg("--replace=aws_instance.foo") {
		t.Errorf("apply invocation missing extra arg, got args: %v", apply.args)
	}
}

func TestRunner_ExtraArgsPassedToDestroy(t *testing.T) {
	cmd, _, invocations := setup(t, map[string]any{"region": "us-east-1"})
	cmd.Args = append(cmd.Args,
		"--command=destroy",
		"--", "--target=aws_instance.foo",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("runner failed unexpectedly: %v\n%s", err, out)
	}
	invs := invocations()
	// Expect: init, destroy.
	if len(invs) != 2 {
		t.Fatalf("expected 2 tofu invocations (init, destroy), got %d: %+v", len(invs), invs)
	}
	destroy := invs[1]
	if !destroy.hasArg("destroy") {
		t.Errorf("second invocation should be 'destroy', got args: %v", destroy.args)
	}
	if !destroy.hasArg("--target=aws_instance.foo") {
		t.Errorf("destroy invocation missing extra arg, got args: %v", destroy.args)
	}
}

func TestRunner_Validate(t *testing.T) {
	bin := runnerBin(t)
	dir := t.TempDir()

	logPath := filepath.Join(dir, "tofu-invocations.log")
	fakeTofuSh := filepath.Join(dir, "tofu.sh")
	script := "#!/bin/sh\n" +
		"{ printf 'cwd=%s\\n' \"$(pwd)\"; for a in \"$@\"; do printf 'arg=%s\\n' \"$a\"; done; printf -- '---\\n'; } >> \"$TOFU_LOG\"\n" +
		"exit 0\n"
	if err := os.WriteFile(fakeTofuSh, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	workTree := filepath.Join(dir, "work")
	const pkgDir = "mypkg"
	if err := os.MkdirAll(filepath.Join(workTree, pkgDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workTree, pkgDir, "main.tf"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	testTmpDir := filepath.Join(dir, "test_tmp")
	if err := os.MkdirAll(testTmpDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin,
		"--tofu="+fakeTofuSh,
		"--work-tree="+workTree,
		"--package-dir="+pkgDir,
		"--command=validate",
	)
	// Deliberately omit BUILD_WORKSPACE_DIRECTORY — validate must not require it.
	env := make([]string, 0, len(os.Environ()))
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "BUILD_WORKSPACE_DIRECTORY=") {
			env = append(env, e)
		}
	}
	cmd.Env = append(env,
		"TEST_TMPDIR="+testTmpDir,
		"TOFU_LOG="+logPath,
	)

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("runner failed unexpectedly: %v\n%s", err, out)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read tofu invocation log: %v", err)
	}
	invs := parseInvocations(data)

	// Expect: init -backend=false, validate.
	if len(invs) != 2 {
		t.Fatalf("expected 2 tofu invocations (init, validate), got %d: %+v", len(invs), invs)
	}
	if !invs[0].hasArg("init") {
		t.Errorf("first invocation should be 'init', got args: %v", invs[0].args)
	}
	if !invs[0].hasArg("-backend=false") {
		t.Errorf("init should have -backend=false, got args: %v", invs[0].args)
	}
	if !invs[1].hasArg("validate") {
		t.Errorf("second invocation should be 'validate', got args: %v", invs[1].args)
	}

	// Both invocations should run inside the copied work tree, not the original.
	wantCwd := filepath.Join(testTmpDir, "work", pkgDir)
	if invs[0].cwd != wantCwd {
		t.Errorf("init cwd = %q, want %q", invs[0].cwd, wantCwd)
	}
	if invs[1].cwd != wantCwd {
		t.Errorf("validate cwd = %q, want %q", invs[1].cwd, wantCwd)
	}

	// The work tree should have been copied into TEST_TMPDIR.
	if _, err := os.Stat(filepath.Join(testTmpDir, "work", pkgDir, "main.tf")); err != nil {
		t.Errorf("expected copied main.tf to exist: %v", err)
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
