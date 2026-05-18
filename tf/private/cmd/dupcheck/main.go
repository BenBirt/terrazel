// Command dupcheck is rules_tofu's build-time guard against variable-key
// collisions across a tf_deploy's `vars` dict and its `var_files`
// entries.
//
// It is invoked from the `tf_deploy` rule via a Bazel build action.
// Inputs:
//   - --vars-key=<name>      (repeatable) — one entry per key in the deploy's
//                                          `vars` dict, known statically.
//   - --var-file=<label>:<path> (repeatable) — `label` is a human-readable
//                                          identifier (typically the file's
//                                          workspace-relative path); `path`
//                                          points at a `.tfvars.json` file
//                                          whose top-level keys are checked.
//   - --stamp=<path>         — file touched on success.
//
// Because `var_files` is restricted to `.tfvars.json` at the rule level,
// JSON parsing of top-level object keys is a complete check; no HCL parser
// is required.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
)

type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

func stringListFlag(name, usage string) *stringList {
	sl := new(stringList)
	flag.Var(sl, name, usage)
	return sl
}

var (
	stamp    = flag.String("stamp", "", "path to a stamp file to touch on success")
	varsKeys = stringListFlag("vars-key", "key from the deploy's `vars` dict (repeatable)")
	varFiles = stringListFlag("var-file", "`label:path` of a .tfvars.json file (repeatable)")
)

func main() {
	flag.Parse()

	if err := run(*varsKeys, *varFiles, *stamp); err != nil {
		fmt.Fprintln(os.Stderr, "rules_tofu: "+err.Error())
		os.Exit(1)
	}
}

func run(varsKeys, varFiles []string, stamp string) error {
	if stamp == "" {
		return errors.New("--stamp is required")
	}

	seen := make(map[string]string, len(varsKeys))
	for _, k := range varsKeys {
		if other, dup := seen[k]; dup {
			return fmt.Errorf(
				"variable %q is declared twice in %s — this should be unreachable for a Starlark dict",
				k, other,
			)
		}
		seen[k] = "vars"
	}

	for _, entry := range varFiles {
		label, path, ok := strings.Cut(entry, ":")
		if !ok {
			return fmt.Errorf("--var-file expected `label:path`, got %q", entry)
		}
		keys, err := readJSONKeys(path)
		if err != nil {
			return fmt.Errorf("read var_file %s: %w", label, err)
		}
		for _, k := range keys {
			if other, dup := seen[k]; dup {
				return fmt.Errorf(
					"variable %q is declared in both %s and %s — "+
						"keys in var_files must not overlap with vars or each other",
					k, other, label,
				)
			}
			seen[k] = label
		}
	}

	return os.WriteFile(stamp, nil, 0o644)
}

func readJSONKeys(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	keys := make([]string, 0, len(top))
	for k := range top {
		keys = append(keys, k)
	}
	return keys, nil
}
