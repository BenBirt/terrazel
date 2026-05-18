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

var dupcheckBinPath = flag.String("dupcheck", "", "path to the dupcheck binary under test")

func dupcheckBin(t *testing.T) string {
	t.Helper()
	if *dupcheckBinPath == "" {
		t.Skip("dupcheck binary path not set; pass -dupcheck=<path> or run via bazel test")
	}
	if filepath.IsAbs(*dupcheckBinPath) {
		return *dupcheckBinPath
	}
	// Under bazel test, the flag is an rlocation path; resolve it against the
	// runfiles directory so exec.Command receives an absolute path.
	if dir := os.Getenv("RUNFILES_DIR"); dir != "" {
		return filepath.Join(dir, *dupcheckBinPath)
	}
	if dir := os.Getenv("TEST_SRCDIR"); dir != "" {
		return filepath.Join(dir, *dupcheckBinPath)
	}
	return *dupcheckBinPath
}

// setup returns a freshly-prepared *exec.Cmd with --stamp wired up, plus a
// helper that writes a .tfvars.json file and returns its path.
func setup(t *testing.T) (cmd *exec.Cmd, stamp string, addVarFile func(string, map[string]any) string) {
	t.Helper()
	bin := dupcheckBin(t)
	dir := t.TempDir()
	stamp = filepath.Join(dir, "stamp")

	cmd = exec.Command(bin, "--stamp="+stamp)

	var counter int
	addVarFile = func(label string, contents map[string]any) string {
		counter++
		path := filepath.Join(dir, fmt.Sprintf("varfile%d.tfvars.json", counter))
		writeJSON(t, path, contents)
		return label + ":" + path
	}
	return cmd, stamp, addVarFile
}

func TestDupcheck_NoVarFiles(t *testing.T) {
	cmd, stamp, _ := setup(t)
	cmd.Args = append(cmd.Args, "--vars-key=region", "--vars-key=env")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("dupcheck failed unexpectedly: %v\n%s", err, out)
	}
	if _, err := os.Stat(stamp); err != nil {
		t.Fatalf("stamp not written: %v", err)
	}
}

func TestDupcheck_VarFileNoOverlap(t *testing.T) {
	cmd, stamp, addVarFile := setup(t)
	cmd.Args = append(cmd.Args,
		"--vars-key=region",
		"--var-file="+addVarFile("secrets.tfvars.json", map[string]any{"db_password": "x"}),
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("dupcheck failed unexpectedly: %v\n%s", err, out)
	}
	if _, err := os.Stat(stamp); err != nil {
		t.Fatalf("stamp not written: %v", err)
	}
}

func TestDupcheck_VarFileOverlapsVars(t *testing.T) {
	cmd, stamp, addVarFile := setup(t)
	cmd.Args = append(cmd.Args,
		"--vars-key=region",
		"--var-file="+addVarFile("override.tfvars.json", map[string]any{"region": "eu-west-1"}),
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected dupcheck to fail on duplicate key, but it succeeded")
	}
	if !strings.Contains(string(out), `"region"`) {
		t.Errorf("expected output to mention the duplicate key, got: %s", out)
	}
	if _, err := os.Stat(stamp); !os.IsNotExist(err) {
		t.Errorf("stamp should not be written on failure, got err=%v", err)
	}
}

func TestDupcheck_VarFilesOverlapEachOther(t *testing.T) {
	cmd, _, addVarFile := setup(t)
	cmd.Args = append(cmd.Args,
		"--var-file="+addVarFile("a.tfvars.json", map[string]any{"env": "prod"}),
		"--var-file="+addVarFile("b.tfvars.json", map[string]any{"env": "staging"}),
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected dupcheck to fail on duplicate key across var files, but it succeeded")
	}
	if !strings.Contains(string(out), `"env"`) {
		t.Errorf("expected output to mention the duplicate key, got: %s", out)
	}
}

func TestDupcheck_MalformedVarFile(t *testing.T) {
	cmd, _, _ := setup(t)
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.tfvars.json")
	if err := os.WriteFile(bad, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd.Args = append(cmd.Args, "--var-file=bad.tfvars.json:"+bad)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected dupcheck to fail on malformed var file, but it succeeded")
	}
	if !strings.Contains(string(out), "bad.tfvars.json") {
		t.Errorf("expected output to mention the offending file, got: %s", out)
	}
}

func TestDupcheck_StampRequired(t *testing.T) {
	bin := dupcheckBin(t)
	cmd := exec.Command(bin, "--vars-key=region")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected dupcheck to fail without --stamp, but it succeeded")
	}
	if !strings.Contains(string(out), "--stamp") {
		t.Errorf("expected output to mention --stamp, got: %s", out)
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
