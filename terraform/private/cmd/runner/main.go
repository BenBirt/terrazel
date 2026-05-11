// Command runner is terrazel's tofu plan/apply wrapper. It is invoked by
// the generated shell launcher emitted by the tf_runner rule with every
// configuration value passed explicitly via CLI flags.
//
// Responsibilities:
//   - Validate we were invoked via `bazel run` (BUILD_WORKSPACE_DIRECTORY set).
//   - Materialize a stable per-target state directory under
//     bazel-out/terrazel/<state-id>/ inside the user's workspace.
//   - Configure TF_PLUGIN_CACHE_DIR so provider plugins are reused across runs.
//   - Run `tofu init` inside the pre-materialized work tree.
//   - For plan: emit a plan artifact and stop.
//   - For apply: re-plan to capture a fresh plan artifact, then apply it.
//   - Serialize concurrent invocations against the same target via flock.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

const usage = `terrazel runner — invoked by the tf_runner rule. Do not run by hand.

Required flags:
  --tofu          Path to the tofu binary.
  --work-tree     Path to the materialized work tree root.
  --package-dir   Workspace-relative directory to cd into before running tofu.
  --state-id      Stable per-target identifier; namespaces state on disk.
  --command       "plan" or "apply".
`

type config struct {
	tofu       string
	workTree   string
	packageDir string
	stateID    string
	command    string
}

func parseFlags(args []string) (*config, error) {
	fs := flag.NewFlagSet("runner", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	c := &config{}
	fs.StringVar(&c.tofu, "tofu", "", "path to the tofu binary")
	fs.StringVar(&c.workTree, "work-tree", "", "path to the materialized work tree root")
	fs.StringVar(&c.packageDir, "package-dir", "", "workspace-relative dir to cd into")
	fs.StringVar(&c.stateID, "state-id", "", "stable per-target identifier")
	fs.StringVar(&c.command, "command", "", `"plan" or "apply"`)
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	missing := []string{}
	if c.tofu == "" {
		missing = append(missing, "--tofu")
	}
	if c.workTree == "" {
		missing = append(missing, "--work-tree")
	}
	if c.packageDir == "" {
		missing = append(missing, "--package-dir")
	}
	if c.stateID == "" {
		missing = append(missing, "--state-id")
	}
	if c.command == "" {
		missing = append(missing, "--command")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required flags: %v", missing)
	}
	if c.command != "plan" && c.command != "apply" {
		return nil, fmt.Errorf("--command must be \"plan\" or \"apply\", got %q", c.command)
	}
	return c, nil
}

func run(c *config, passthrough []string) error {
	workspace := os.Getenv("BUILD_WORKSPACE_DIRECTORY")
	if workspace == "" {
		return errors.New(
			"terrazel runner must be invoked via `bazel run`. " +
				"BUILD_WORKSPACE_DIRECTORY is unset, so state cannot be persisted.",
		)
	}

	stateDir := filepath.Join(workspace, "bazel-out", "terrazel", c.stateID)
	pluginCache := filepath.Join(workspace, "bazel-out", "terrazel", "plugin-cache")
	lockPath := filepath.Join(workspace, "bazel-out", "terrazel", "locks", c.stateID+".lock")
	for _, d := range []string{stateDir, pluginCache, filepath.Dir(lockPath)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", d, err)
		}
	}

	unlock, err := acquireLock(lockPath)
	if err != nil {
		return fmt.Errorf("acquire lock %s: %w", lockPath, err)
	}
	defer unlock()

	cwd := filepath.Join(c.workTree, c.packageDir)
	if _, err := os.Stat(cwd); err != nil {
		return fmt.Errorf("work-tree cwd %s: %w", cwd, err)
	}

	env := append(os.Environ(),
		"TF_PLUGIN_CACHE_DIR="+pluginCache,
		"TF_IN_AUTOMATION=1",
		"TF_INPUT=0",
		// We don't (yet) treat .terraform.lock.hcl as a first-class
		// input, so allow the plugin cache to satisfy init without
		// demanding lockfile-matching hashes.
		"TF_PLUGIN_CACHE_MAY_BREAK_DEPENDENCY_LOCK_FILE=1",
	)

	if err := tofu(c.tofu, env, cwd, "init", "-input=false"); err != nil {
		return fmt.Errorf("tofu init: %w", err)
	}

	planFile := filepath.Join(stateDir, "tfplan")
	stateFile := filepath.Join(stateDir, "terraform.tfstate")
	stateArgs := []string{"-state=" + stateFile, "-state-out=" + stateFile}
	if hasBackend(cwd) {
		stateArgs = nil
	}

	switch c.command {
	case "plan":
		args := append([]string{"plan", "-input=false", "-out=" + planFile}, stateArgs...)
		args = append(args, passthrough...)
		if err := tofu(c.tofu, env, cwd, args...); err != nil {
			return fmt.Errorf("tofu plan: %w", err)
		}
		fmt.Printf("terrazel: plan saved to %s\n", planFile)
	case "apply":
		// Always re-plan so apply consumes a freshly captured plan.
		planArgs := append([]string{"plan", "-input=false", "-out=" + planFile}, stateArgs...)
		if err := tofu(c.tofu, env, cwd, planArgs...); err != nil {
			return fmt.Errorf("tofu plan (for apply): %w", err)
		}
		applyArgs := append([]string{"apply", "-input=false"}, stateArgs...)
		applyArgs = append(applyArgs, planFile)
		if err := tofu(c.tofu, env, cwd, applyArgs...); err != nil {
			return fmt.Errorf("tofu apply: %w", err)
		}
	}
	return nil
}

func tofu(bin string, env []string, cwd string, args ...string) error {
	cmd := exec.Command(bin, args...)
	cmd.Dir = cwd
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// hasBackend returns true if any .tf file at the deploy's cwd declares a
// `backend "<name>" {` block. We only scan the cwd (not recursively) because
// the backend block must live in the root module.
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

// containsBackendBlock looks for a top-level `backend "..." {` block.
// Crude but sufficient: a real HCL parser would be heavyweight for a
// boolean check, and false positives just mean we skip our state flags.
func containsBackendBlock(b []byte) bool {
	s := string(b)
	for i := 0; i+8 < len(s); i++ {
		// Match start-of-line `backend "`.
		if (i == 0 || s[i-1] == '\n') && len(s)-i >= 9 && s[i:i+8] == "backend " && s[i+8] == '"' {
			return true
		}
	}
	return false
}

func acquireLock(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}

func main() {
	log.SetFlags(0)
	log.SetPrefix("terrazel: ")

	// Split our flags from passthrough args. Anything after the first
	// non-flag (or after `--`) is forwarded to `tofu plan`. We use
	// flag.Parse-style behavior: stop at the first non-flag argument.
	args := os.Args[1:]
	var ours, theirs []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--" {
			theirs = append(theirs, args[i+1:]...)
			break
		}
		if len(args[i]) > 0 && args[i][0] == '-' {
			ours = append(ours, args[i])
			continue
		}
		// First positional: stop ours, rest is theirs.
		theirs = append(theirs, args[i:]...)
		break
	}

	c, err := parseFlags(ours)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprint(os.Stderr, "\n", usage)
		os.Exit(2)
	}

	if err := run(c, theirs); err != nil {
		fmt.Fprintln(os.Stderr, err)
		// If the inner tofu exited non-zero, preserve its exit code
		// where possible.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		os.Exit(1)
	}
}
