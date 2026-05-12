// Command runner is terrazel's tofu plan/apply wrapper. It is invoked by
// the generated shell launcher emitted by the tf_runner rule with every
// configuration value passed via explicit standard flags.
//
// Responsibilities:
//   - Validate we were invoked via `bazel run` (BUILD_WORKSPACE_DIRECTORY set).
//   - Run `tofu init` inside the pre-materialized work tree.
//   - For plan: emit a plan artifact and stop.
//   - For apply: re-plan to capture a fresh plan artifact, then apply it.
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

var (
	flagTofu       = flag.String("tofu", "", "path to the tofu binary")
	flagWorkTree   = flag.String("work-tree", "", "path to the materialized work tree root")
	flagPackageDir = flag.String("package-dir", "", "workspace-relative dir to cd into within the work tree")
	flagStateDir   = flag.String("state-dir", "", "absolute path to the per-deploy state directory")
	flagCommand    = flag.String("command", "", `"plan", "apply", or "destroy"`)
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
	for name, value := range map[string]string{
		"--tofu":        *flagTofu,
		"--work-tree":   *flagWorkTree,
		"--package-dir": *flagPackageDir,
		"--state-dir":   *flagStateDir,
		"--command":     *flagCommand,
	} {
		if value == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	if *flagCommand != "plan" && *flagCommand != "apply" && *flagCommand != "destroy" {
		return fmt.Errorf(`--command must be "plan", "apply", or "destroy", got %q`, *flagCommand)
	}

	if os.Getenv("BUILD_WORKSPACE_DIRECTORY") == "" {
		return errors.New(
			"terrazel runner must be invoked via `bazel run`. " +
				"BUILD_WORKSPACE_DIRECTORY is unset, so state cannot be persisted.",
		)
	}

	if err := os.MkdirAll(*flagStateDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", *flagStateDir, err)
	}

	cwd := filepath.Join(*flagWorkTree, *flagPackageDir)
	if _, err := os.Stat(cwd); err != nil {
		return fmt.Errorf("work-tree cwd %s: %w", cwd, err)
	}

	// TF_IN_AUTOMATION=1 suppresses interactive-style usage hints in
	// tofu's output (e.g. "Run `terraform plan` to ..." lines that
	// wouldn't apply in our wrapped invocation).
	// TF_INPUT=0 disables every prompt — variable prompts, confirmation
	// prompts, init's "ask about backend changes" — so the process
	// never blocks waiting for stdin.
	env := append(os.Environ(),
		"TF_IN_AUTOMATION=1",
		"TF_INPUT=0",
	)

	if err := tofu(env, cwd, "init", "-input=false"); err != nil {
		return fmt.Errorf("tofu init: %w", err)
	}

	planFile := filepath.Join(*flagStateDir, "tfplan")
	stateFile := filepath.Join(*flagStateDir, "terraform.tfstate")
	stateArgs := []string{"-state=" + stateFile, "-state-out=" + stateFile}
	if hasBackend(cwd) {
		stateArgs = nil
	}

	switch *flagCommand {
	case "plan":
		args := append([]string{"plan", "-input=false", "-out=" + planFile}, stateArgs...)
		if err := tofu(env, cwd, args...); err != nil {
			return fmt.Errorf("tofu plan: %w", err)
		}
		fmt.Printf("terrazel: plan saved to %s\n", planFile)
	case "apply":
		// Always re-plan so apply consumes a freshly captured plan.
		planArgs := append([]string{"plan", "-input=false", "-out=" + planFile}, stateArgs...)
		if err := tofu(env, cwd, planArgs...); err != nil {
			return fmt.Errorf("tofu plan (for apply): %w", err)
		}
		applyArgs := append([]string{"apply", "-input=false"}, stateArgs...)
		applyArgs = append(applyArgs, planFile)
		if err := tofu(env, cwd, applyArgs...); err != nil {
			return fmt.Errorf("tofu apply: %w", err)
		}
	case "destroy":
		destroyArgs := append([]string{"destroy", "-auto-approve"}, stateArgs...)
		if err := tofu(env, cwd, destroyArgs...); err != nil {
			return fmt.Errorf("tofu destroy: %w", err)
		}
	}
	return nil
}

func tofu(env []string, cwd string, args ...string) error {
	cmd := exec.Command(*flagTofu, args...)
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
