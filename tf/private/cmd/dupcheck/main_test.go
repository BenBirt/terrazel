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

// dupcheckCmd wraps an exec.Cmd for the dupcheck binary with helpers for
// writing var files into a temp directory and reading the stamp path.
type dupcheckCmd struct {
	Cmd     *exec.Cmd
	Stamp   string
	t       *testing.T
	dir     string
	counter int
}

func setup(t *testing.T) *dupcheckCmd {
	t.Helper()
	bin := dupcheckBin(t)
	dir := t.TempDir()
	stamp := filepath.Join(dir, "stamp")
	return &dupcheckCmd{
		Cmd:   exec.Command(bin, "--stamp="+stamp),
		Stamp: stamp,
		t:     t,
		dir:   dir,
	}
}

// AddVarFile writes a .tfvars.json file with the given JSON contents and
// returns the "label:path" flag value ready to pass as --var-file.
func (d *dupcheckCmd) addVarFile(label string, contents map[string]any) string {
	d.t.Helper()
	d.counter++
	path := filepath.Join(d.dir, fmt.Sprintf("varfile%d.tfvars.json", d.counter))
	writeJSON(d.t, path, contents)
	return label + ":" + path
}

// AddHCLVarFile writes a .tfvars file with the given raw HCL content and
// returns the "label:path" flag value ready to pass as --var-file.
func (d *dupcheckCmd) addHCLVarFile(label string, content string) string {
	d.t.Helper()
	d.counter++
	path := filepath.Join(d.dir, fmt.Sprintf("varfile%d.tfvars", d.counter))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		d.t.Fatal(err)
	}
	return label + ":" + path
}

func TestDupcheck_NoVarFiles(t *testing.T) {
	d := setup(t)
	d.Cmd.Args = append(d.Cmd.Args, "--vars-key=region", "--vars-key=env")
	if out, err := d.Cmd.CombinedOutput(); err != nil {
		t.Fatalf("dupcheck failed unexpectedly: %v\n%s", err, out)
	}
	if _, err := os.Stat(d.Stamp); err != nil {
		t.Fatalf("stamp not written: %v", err)
	}
}

func TestDupcheck_VarFileNoOverlap(t *testing.T) {
	d := setup(t)
	d.Cmd.Args = append(d.Cmd.Args,
		"--vars-key=region",
		"--var-file="+d.addVarFile("secrets.tfvars.json", map[string]any{"db_password": "x"}),
	)
	if out, err := d.Cmd.CombinedOutput(); err != nil {
		t.Fatalf("dupcheck failed unexpectedly: %v\n%s", err, out)
	}
	if _, err := os.Stat(d.Stamp); err != nil {
		t.Fatalf("stamp not written: %v", err)
	}
}

func TestDupcheck_VarFileOverlapsVars(t *testing.T) {
	d := setup(t)
	d.Cmd.Args = append(d.Cmd.Args,
		"--vars-key=region",
		"--var-file="+d.addVarFile("override.tfvars.json", map[string]any{"region": "eu-west-1"}),
	)
	out, err := d.Cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected dupcheck to fail on duplicate key, but it succeeded")
	}
	if !strings.Contains(string(out), `"region"`) {
		t.Errorf("expected output to mention the duplicate key, got: %s", out)
	}
	if _, err := os.Stat(d.Stamp); !os.IsNotExist(err) {
		t.Errorf("stamp should not be written on failure, got err=%v", err)
	}
}

func TestDupcheck_VarFilesOverlapEachOther(t *testing.T) {
	d := setup(t)
	d.Cmd.Args = append(d.Cmd.Args,
		"--var-file="+d.addVarFile("a.tfvars.json", map[string]any{"env": "prod"}),
		"--var-file="+d.addVarFile("b.tfvars.json", map[string]any{"env": "staging"}),
	)
	out, err := d.Cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected dupcheck to fail on duplicate key across var files, but it succeeded")
	}
	if !strings.Contains(string(out), `"env"`) {
		t.Errorf("expected output to mention the duplicate key, got: %s", out)
	}
}

func TestDupcheck_MalformedVarFile(t *testing.T) {
	d := setup(t)
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.tfvars.json")
	if err := os.WriteFile(bad, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	d.Cmd.Args = append(d.Cmd.Args, "--var-file=bad.tfvars.json:"+bad)
	out, err := d.Cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected dupcheck to fail on malformed var file, but it succeeded")
	}
	if !strings.Contains(string(out), "bad.tfvars.json") {
		t.Errorf("expected output to mention the offending file, got: %s", out)
	}
}

func TestDupcheck_HCLVarFileNoOverlap(t *testing.T) {
	d := setup(t)
	d.Cmd.Args = append(d.Cmd.Args,
		"--vars-key=region",
		"--var-file="+d.addHCLVarFile("secrets.tfvars", `
			db_password = "x"
			db_username = "admin"
		`),
	)
	if out, err := d.Cmd.CombinedOutput(); err != nil {
		t.Fatalf("dupcheck failed unexpectedly: %v\n%s", err, out)
	}
	if _, err := os.Stat(d.Stamp); err != nil {
		t.Fatalf("stamp not written: %v", err)
	}
}

func TestDupcheck_HCLVarFileOverlapsVars(t *testing.T) {
	d := setup(t)
	d.Cmd.Args = append(d.Cmd.Args,
		"--vars-key=region",
		"--var-file="+d.addHCLVarFile("override.tfvars", `
			region = "eu-west-1"
		`),
	)
	out, err := d.Cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected dupcheck to fail on duplicate key, but it succeeded")
	}
	if !strings.Contains(string(out), `"region"`) {
		t.Errorf("expected output to mention the duplicate key, got: %s", out)
	}
	if _, err := os.Stat(d.Stamp); !os.IsNotExist(err) {
		t.Errorf("stamp should not be written on failure, got err=%v", err)
	}
}

func TestDupcheck_HCLVarFilesOverlapEachOther(t *testing.T) {
	d := setup(t)
	d.Cmd.Args = append(d.Cmd.Args,
		"--var-file="+d.addHCLVarFile("a.tfvars", `env = "prod"`),
		"--var-file="+d.addHCLVarFile("b.tfvars", `env = "staging"`),
	)
	out, err := d.Cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected dupcheck to fail on duplicate key across var files, but it succeeded")
	}
	if !strings.Contains(string(out), `"env"`) {
		t.Errorf("expected output to mention the duplicate key, got: %s", out)
	}
}

func TestDupcheck_HCLMalformedVarFile(t *testing.T) {
	d := setup(t)
	d.Cmd.Args = append(d.Cmd.Args,
		"--var-file="+d.addHCLVarFile("bad.tfvars", `
			# Missing value is invalid HCL
			invalid_assignment = 
		`),
	)
	out, err := d.Cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected dupcheck to fail on malformed var file, but it succeeded")
	}
	if !strings.Contains(string(out), "bad.tfvars") {
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
