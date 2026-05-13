// Command runner is terrazel's tofu plan/apply wrapper. It is invoked by
// the generated shell launcher emitted by the tf_runner rule with every
// configuration value passed via explicit standard flags.
//
// Responsibilities:
//   - Validate we were invoked via `bazel run` (BUILD_WORKSPACE_DIRECTORY set).
//   - Run `tofu init` inside the pre-materialized work tree.
//   - For plan: emit a plan artifact and stop.
//   - For apply/destroy: delegate to tofu, passing through any extra args.
//
// Configuration validation (`tofu init -backend=false && tofu validate`)
// happens at `bazel build` time via the deploy rule's TofuValidate action,
// not here.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// stringList is a repeatable string flag (e.g. --var-file can appear multiple times).
type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

func stringListFlag(name, usage string) *stringList {
	sl := new(stringList)
	flag.Var(sl, name, usage)
	return sl
}

var (
	workTree   = flag.String("work-tree", "", "path to the materialized work tree root")
	packageDir = flag.String("package-dir", "", "workspace-relative dir to cd into within the work tree")
	pluginDir  = flag.String("plugin-dir", "", "absolute path to the vendored provider plugin tree, passed to tofu init via -plugin-dir")
	varFiles   = stringListFlag("var-file", "path to a .tfvars.json file passed to tofu via -var-file (repeatable)")

	stateDir = flag.String("state-dir", "", "absolute path to the per-deploy state directory")

	tofu    = flag.String("tofu", "", "path to the tofu binary")
	command = flag.String("command", "", `"plan", "apply", or "destroy"`)
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("terrazel: ")
	flag.Parse()

	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		os.Exit(1)
	}
}

func run() error {
	extraArgs := flag.Args()

	for name, value := range map[string]string{
		"--tofu":        *tofu,
		"--work-tree":   *workTree,
		"--package-dir": *packageDir,
		"--command":     *command,
		"--state-dir":   *stateDir,
		"--plugin-dir":  *pluginDir,
	} {
		if value == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	if *command != "plan" && *command != "apply" && *command != "destroy" {
		return fmt.Errorf(`--command must be "plan", "apply", or "destroy", got %q`, *command)
	}

	if os.Getenv("BUILD_WORKSPACE_DIRECTORY") == "" {
		return errors.New(
			"terrazel runner must be invoked via `bazel run`. " +
				"BUILD_WORKSPACE_DIRECTORY is unset, so state cannot be persisted.",
		)
	}
	if err := os.MkdirAll(*stateDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", *stateDir, err)
	}

	cwd := filepath.Join(*workTree, *packageDir)
	if _, err := os.Stat(cwd); err != nil {
		return fmt.Errorf("work-tree cwd %s: %w", cwd, err)
	}

	// TF_IN_AUTOMATION=1 suppresses usage-hint lines in tofu output that
	// don't apply in our wrapped invocation (e.g. "Run `terraform plan`…").
	// We deliberately do NOT set TF_INPUT=0 globally: variable prompts are
	// suppressed per-command via -input=false, but the apply/destroy
	// approval prompt must remain reachable.
	env := append(os.Environ(), "TF_IN_AUTOMATION=1")

	if err := checkVarFileDuplicates(cwd, *varFiles); err != nil {
		return err
	}
	// Ensure the vendored plugin tree exists even when zero providers are in
	// scope (the deploy declares no symlinks under .terrazel-plugins/ in that
	// case, so the runfiles tree lacks the directory). -plugin-dir overrides
	// all default plugin search paths and prevents the registry from being
	// contacted at runtime.
	if err := os.MkdirAll(*pluginDir, 0o755); err != nil {
		return fmt.Errorf("create plugin dir %s: %w", *pluginDir, err)
	}
	if err := runTofu(env, cwd, "init", "-input=false", "-plugin-dir="+*pluginDir); err != nil {
		return fmt.Errorf("tofu init: %w", err)
	}
	planFile := filepath.Join(*stateDir, "tfplan")
	stateFile := filepath.Join(*stateDir, "terraform.tfstate")
	stateArgs := []string{"-state=" + stateFile, "-state-out=" + stateFile}
	if hasBackend(cwd) {
		stateArgs = nil
	}
	varFileArgs := make([]string, len(*varFiles))
	for i, f := range *varFiles {
		varFileArgs[i] = "-var-file=" + f
	}

	switch *command {
	case "plan":
		args := append([]string{"plan", "-input=false", "-out=" + planFile}, varFileArgs...)
		args = append(args, stateArgs...)
		args = append(args, extraArgs...)
		if err := runTofu(env, cwd, args...); err != nil {
			return fmt.Errorf("tofu plan: %w", err)
		}
		fmt.Printf("terrazel: plan saved to %s\n", planFile)
	case "apply":
		applyArgs := append([]string{"apply", "-input=false"}, varFileArgs...)
		applyArgs = append(applyArgs, stateArgs...)
		applyArgs = append(applyArgs, extraArgs...)
		if err := runTofu(env, cwd, applyArgs...); err != nil {
			return fmt.Errorf("tofu apply: %w", err)
		}
	case "destroy":
		destroyArgs := append([]string{"destroy", "-input=false"}, varFileArgs...)
		destroyArgs = append(destroyArgs, stateArgs...)
		destroyArgs = append(destroyArgs, extraArgs...)
		if err := runTofu(env, cwd, destroyArgs...); err != nil {
			return fmt.Errorf("tofu destroy: %w", err)
		}
	}
	return nil
}

// checkVarFileDuplicates errors if any variable key appears more than once across
// the generated terrazel.auto.tfvars.json (from vars) and the supplied var files.
// All files are .tfvars.json so JSON parsing is sufficient for complete detection.
//
// TODO: move this check into a bazel build action so duplicates fail at
// `bazel build` time (with caching) rather than at `bazel run` time.
func checkVarFileDuplicates(cwd string, varFiles []string) error {
	type source struct {
		label string
		keys  map[string]struct{}
	}

	readJSONKeys := func(path string) (map[string]struct{}, error) {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var top map[string]json.RawMessage
		if err := json.Unmarshal(data, &top); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		keys := make(map[string]struct{}, len(top))
		for k := range top {
			keys[k] = struct{}{}
		}
		return keys, nil
	}

	tfvarsPath := filepath.Join(cwd, "terrazel.auto.tfvars.json")
	tfvarsKeys, err := readJSONKeys(tfvarsPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read terrazel.auto.tfvars.json: %w", err)
	}

	sources := []source{{"vars", tfvarsKeys}}
	for _, f := range varFiles {
		keys, err := readJSONKeys(f)
		if err != nil {
			return fmt.Errorf("read var_file %s: %w", f, err)
		}
		sources = append(sources, source{f, keys})
	}

	for i, a := range sources {
		for j, b := range sources {
			if j <= i {
				continue
			}
			for k := range a.keys {
				if _, dup := b.keys[k]; dup {
					return fmt.Errorf(
						"variable %q is declared in both %s and %s — "+
							"keys in var_files must not overlap with vars or each other",
						k, a.label, b.label,
					)
				}
			}
		}
	}
	return nil
}

func runTofu(env []string, cwd string, args ...string) error {
	cmd := exec.Command(*tofu, args...)
	cmd.Dir = cwd
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// hasBackend reports whether any .tf file in the deploy's package
// declares a top-level `backend "..." {` block. Crude but sufficient: a
// real HCL parser would be heavyweight for a boolean check, and false
// positives just mean we let the configured backend own state.
func hasBackend(cwd string) bool {
	entries, err := os.ReadDir(cwd)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".tf" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(cwd, e.Name()))
		if err != nil {
			continue
		}
		if containsBackendBlock(b) {
			return true
		}
	}
	return false
}

func containsBackendBlock(b []byte) bool {
	s := string(b)
	for i := 0; i+9 < len(s); i++ {
		if (i == 0 || s[i-1] == '\n') && s[i:i+8] == "backend " && s[i+8] == '"' {
			return true
		}
	}
	return false
}
