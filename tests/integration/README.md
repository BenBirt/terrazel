# Negative integration tests

Bazel-in-Bazel tests that assert misuses of `tf_library` /
`tf_deploy` fail with the error messages the rules promise.

## Layout

```
tests/integration/
├── BUILD.bazel              bazel_integration_test target per case
├── assert_build_fails.sh    runs the nested bazel and asserts
│                            non-zero exit + stderr fragment
└── broken_workspace/        sub-workspace for the nested bazel
    ├── MODULE.bazel
    ├── .bazelversion
    └── cases/
        ├── auto_tfvars_conflict/{BUILD.bazel, main.tf, rules_tofu.auto.tfvars.json}
        ├── conflicting_providers/{BUILD.bazel, main.tf}
        ├── duplicate_vars/{BUILD.bazel, main.tf, extra.tfvars.json}
        ├── external_module_ref/{BUILD.bazel, main.tf}
        ├── lock_file_smuggled/{BUILD.bazel, main.tf, .terraform.lock.hcl}
        ├── malformed_var_file/{BUILD.bazel, main.tf, bad.tfvars.json}
        ├── missing_provider/{BUILD.bazel, main.tf}
        └── validate_fails/{BUILD.bazel, main.tf}
```

The child workspace packages are excluded from the parent Bazel via
the `--deleted_packages` flag in `.bazelrc` (the "deleted packages"
trick recommended by `rules_bazel_integration_test`), so the outer
Bazel never analyzes the intentionally-failing case targets when
expanding `//...`.

## Running

```sh
bazel test //tests/integration:all
```

Each test spawns a nested Bazel that materializes the case's work tree,
runs `tofu init/validate` (or the dupcheck action), and the driver
checks the failure mode.

## Adding a case

1. Create `broken_workspace/cases/<case>/` with a `BUILD.bazel` that
   misuses the rules, plus any fixture files the failure mode needs.
2. Add a row to `_CASES` in `tests/integration/BUILD.bazel`:
   `("<case>", "//cases/<case>:<target>", "<stderr-fragment>")`.
3. Execute `bazel run @rules_bazel_integration_test//tools:update_deleted_packages`
   to add the new package to `--deleted_packages` in the root `.bazelrc`.

Pick a stderr fragment short and stable enough not to break on tofu /
rule-error wording tweaks; avoid quoting full sentences.

## Follow-ups

All core build-time negative failure modes are now fully tested!

The remaining non-build execution modes (`bazel run :deploy.plan` when run outside of the Bazel environment, or `tf_providers` module resolution errors) can be addressed in future testing passes if needed.

