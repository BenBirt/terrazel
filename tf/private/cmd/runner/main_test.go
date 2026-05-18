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
// rules_tofu.auto.tfvars.json, and a fake tofu script that logs each invocation
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
	writeJSON(t, filepath.Join(workTree, pkgDir, "rules_tofu.auto.tfvars.json"), vars)

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

	pluginDir := filepath.Join(workTree, pkgDir, ".rules_tofu-plugins")

	cmd = exec.Command(bin,
		"--tofu="+fakeTofuSh,
		"--work-tree="+workTree,
		"--package-dir="+pkgDir,
		"--plugin-dir="+pluginDir,
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
	init := invs[0]
	if !init.hasArg("init") {
		t.Errorf("first invocation should be 'init', got args: %v", init.args)
	}
	if !init.hasArg("-input=false") {
		t.Errorf("init invocation missing -input=false, got args: %v", init.args)
	}
	if !init.hasArgWithPrefix("-plugin-dir=") {
		t.Errorf("init invocation missing -plugin-dir=, got args: %v", init.args)
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

// TestRunner_StateFlags_Apply verifies that the apply invocation receives
// -state= and -state-out= flags when no backend block is present.
func TestRunner_StateFlags_Apply(t *testing.T) {
	cmd, _, invocations := setup(t, map[string]any{"region": "us-east-1"})
	cmd.Args = append(cmd.Args, "--command=apply")
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
	if !apply.hasArgWithPrefix("-state=") {
		t.Errorf("apply invocation missing -state=, got args: %v", apply.args)
	}
	if !apply.hasArgWithPrefix("-state-out=") {
		t.Errorf("apply invocation missing -state-out=, got args: %v", apply.args)
	}
}

// TestRunner_StateFlags_Destroy verifies that the destroy invocation receives
// -state= and -state-out= flags when no backend block is present.
func TestRunner_StateFlags_Destroy(t *testing.T) {
	cmd, _, invocations := setup(t, map[string]any{"region": "us-east-1"})
	cmd.Args = append(cmd.Args, "--command=destroy")
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
	if !destroy.hasArgWithPrefix("-state=") {
		t.Errorf("destroy invocation missing -state=, got args: %v", destroy.args)
	}
	if !destroy.hasArgWithPrefix("-state-out=") {
		t.Errorf("destroy invocation missing -state-out=, got args: %v", destroy.args)
	}
}

// backendTFContent is a helper that writes a .tf file with a terraform block
// containing the given inner content into the package directory of the work
// tree, and returns the path written.
func backendTFContent(t *testing.T, cmd *exec.Cmd, content string) {
	t.Helper()
	var workTree string
	for _, arg := range cmd.Args {
		if strings.HasPrefix(arg, "--work-tree=") {
			workTree = strings.TrimPrefix(arg, "--work-tree=")
		}
	}
	if workTree == "" {
		t.Fatal("could not find --work-tree flag in runner cmd args")
	}
	tfPath := filepath.Join(workTree, "mypkg", "backend_override.tf")
	if err := os.WriteFile(tfPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestRunner_BackendBlock_OmitsStateFlags verifies that when a terraform{}
// backend "..." {} block is present, the runner does not pass -state= or
// -state-out= to tofu.
func TestRunner_BackendBlock_OmitsStateFlags(t *testing.T) {
	cmd, _, invocations := setup(t, map[string]any{"region": "us-east-1"})
	backendTFContent(t, cmd, `
terraform {
  backend "local" {}
}
`)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("runner failed unexpectedly: %v\n%s", err, out)
	}
	invs := invocations()
	if len(invs) != 2 {
		t.Fatalf("expected 2 tofu invocations (init, plan), got %d: %+v", len(invs), invs)
	}
	plan := invs[1]
	if plan.hasArgWithPrefix("-state=") {
		t.Errorf("plan invocation should not have -state= when backend block present, got args: %v", plan.args)
	}
	if plan.hasArgWithPrefix("-state-out=") {
		t.Errorf("plan invocation should not have -state-out= when backend block present, got args: %v", plan.args)
	}
}

// TestRunner_CloudBlock_OmitsStateFlags verifies that when a terraform{}
// cloud {} block is present (the modern HCP Terraform alternative to backend),
// the runner does not pass -state= or -state-out= to tofu.
func TestRunner_CloudBlock_OmitsStateFlags(t *testing.T) {
	cmd, _, invocations := setup(t, map[string]any{"region": "us-east-1"})
	backendTFContent(t, cmd, `
terraform {
  cloud {}
}
`)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("runner failed unexpectedly: %v\n%s", err, out)
	}
	invs := invocations()
	if len(invs) != 2 {
		t.Fatalf("expected 2 tofu invocations (init, plan), got %d: %+v", len(invs), invs)
	}
	plan := invs[1]
	if plan.hasArgWithPrefix("-state=") {
		t.Errorf("plan invocation should not have -state= when cloud block present, got args: %v", plan.args)
	}
	if plan.hasArgWithPrefix("-state-out=") {
		t.Errorf("plan invocation should not have -state-out= when cloud block present, got args: %v", plan.args)
	}
}

// TestRunner_MissingBuildWorkspaceDirectory verifies that the runner exits
// non-zero and prints an actionable message when BUILD_WORKSPACE_DIRECTORY
// is not set (i.e. it was invoked outside of `bazel run`).
func TestRunner_MissingBuildWorkspaceDirectory(t *testing.T) {
	cmd, _, _ := setup(t, map[string]any{"region": "us-east-1"})
	// Strip BUILD_WORKSPACE_DIRECTORY from the environment.
	filtered := cmd.Env[:0]
	for _, e := range cmd.Env {
		if !strings.HasPrefix(e, "BUILD_WORKSPACE_DIRECTORY=") {
			filtered = append(filtered, e)
		}
	}
	cmd.Env = filtered
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected runner to fail when BUILD_WORKSPACE_DIRECTORY is unset, but it succeeded")
	}
	msg := string(out)
	if !strings.Contains(msg, "BUILD_WORKSPACE_DIRECTORY") && !strings.Contains(msg, "bazel run") {
		t.Errorf("expected output to mention BUILD_WORKSPACE_DIRECTORY or bazel run, got: %s", msg)
	}
}

// TestRunner_MissingRequiredFlags verifies that omitting any single required
// flag causes the runner to exit non-zero with a message naming the flag.
func TestRunner_MissingRequiredFlags(t *testing.T) {
	requiredFlags := []string{
		"--tofu",
		"--work-tree",
		"--package-dir",
		"--command",
		"--state-dir",
		"--plugin-dir",
	}
	for _, flagName := range requiredFlags {
		flagName := flagName
		t.Run(flagName, func(t *testing.T) {
			cmd, _, _ := setup(t, map[string]any{"region": "us-east-1"})
			// Remove the flag from Args, keeping all others.
			filtered := cmd.Args[:1] // keep argv[0] (binary path)
			for _, arg := range cmd.Args[1:] {
				if strings.HasPrefix(arg, flagName+"=") || arg == flagName {
					continue
				}
				filtered = append(filtered, arg)
			}
			cmd.Args = filtered
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("expected runner to fail when %s is omitted, but it succeeded", flagName)
			}
			if !strings.Contains(string(out), flagName) {
				t.Errorf("expected output to mention %s, got: %s", flagName, out)
			}
		})
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
