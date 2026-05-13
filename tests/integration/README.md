# Negative integration tests

Bazel-in-Bazel tests that assert misuses of `terraform_library` /
`terraform_deploy` fail with the error messages the rules promise.

## Layout

```
tests/integration/
├── BUILD.bazel              bazel_integration_test target per case
├── assert_build_fails.sh    stages BUILD.bazel files from BUILD.tpl, runs
                             the nested bazel, asserts non-zero exit and a
                             stderr fragment match
└── broken_workspace/        sub-workspace for the nested bazel
    ├── MODULE.bazel.tpl
    ├── .bazelversion
    └── cases/
        ├── duplicate_vars/{BUILD.tpl, main.tf, extra.tfvars.json}
        ├── validate_fails/{BUILD.tpl, main.tf}
        ├── missing_provider/{BUILD.tpl, main.tf}
        └── lock_file_smuggled/{BUILD.tpl, main.tf, .terraform.lock.hcl}
```

BUILD and MODULE files ship with a `.tpl` extension so the outer terrazel
Bazel never analyzes intentionally-failing targets or treats
`broken_workspace/MODULE.bazel` as a repo-boundary marker. The test
driver materializes the real filenames inside the staged workspace just
before invoking the nested Bazel.

## Running

In CI (and any environment with outbound network):

```sh
bazel test //tests/integration:all
```

Each test spawns a nested Bazel that materializes the case's work tree,
runs `tofu init/validate` (or the dupcheck action), and the driver
checks the failure mode.

In a network-restricted sandbox the nested Bazel will fail to fetch its
own dependencies — same constraint as the top-level `bazel build //...`.
No special tag gates these tests; they just won't pass without network.

## Adding a case

1. Create `broken_workspace/cases/<case>/` with a `BUILD.tpl` (note the
   extension — *not* `BUILD.bazel`) that misuses the rules, plus any
   fixture files the failure mode needs.
2. Add a row to `_CASES` in `tests/integration/BUILD.bazel`:
   `("<case>", "//cases/<case>:<target>", "<stderr-fragment>")`.

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
