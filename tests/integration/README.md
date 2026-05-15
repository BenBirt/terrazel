# Negative integration tests

Bazel-in-Bazel tests that assert misuses of `terraform_library` /
`terraform_deploy` fail with the error messages the rules promise.

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
        ├── duplicate_vars/{BUILD.bazel, main.tf, extra.tfvars.json}
        ├── validate_fails/{BUILD.bazel, main.tf}
        ├── missing_provider/{BUILD.bazel, main.tf}
        └── lock_file_smuggled/{BUILD.bazel, main.tf, .terraform.lock.hcl}
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

Failure modes the rules raise that we don't cover yet. Same machinery —
each is one new row plus a fixture directory:

| Case | Source of error | Fragment |
|---|---|---|
| File collision between deps with same workspace path | `terraform/private/work_tree.bzl:54-63` | `collision at workspace path` |
| Conflicting provider versions across deps | `terraform/private/work_tree.bzl:106-117` | `conflicting versions` |
| `terrazel.auto.tfvars.json` in srcs | `terraform/private/work_tree.bzl:70-77` | `collides with the generated` |
| External-module file reference | `terraform/private/work_tree.bzl:45-53` | `external Bazel module` |
| Malformed JSON in `var_files` | `terraform/private/cmd/dupcheck/main.go:96-104` | `parse:` |
| Runner not invoked via `bazel run` | `terraform/private/cmd/runner/main.go:86-90` | `BUILD_WORKSPACE_DIRECTORY` |

The runner-not-via-`bazel-run` case needs a separate driver because the
assertion is on `bazel run :<deploy>.plan`, not `bazel build`.

`terraform_providers` extension errors (bad source format, unknown
platform key, ambiguous binary in
`terraform/providers/extensions.bzl`) fail during outer module
resolution rather than at build time. Covering them needs a second
sub-workspace whose `MODULE.bazel` is itself malformed plus a driver
that runs e.g. `bazel mod graph` and asserts failure. Distinct enough
that it's worth its own pass.
