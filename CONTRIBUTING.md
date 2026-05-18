# Contributing to rules_tofu

Thank you for your interest in contributing!

## Development setup

You need:

- [Bazel](https://bazel.build/) (version pinned in [`.bazelversion`](.bazelversion))
- A C toolchain (for `rules_go` compilation)

Clone the repo and build everything:

```sh
git clone https://github.com/benbirt/terrazel.git
cd terrazel
bazel build //...
bazel test //...
```

## Repository layout

| Path | Purpose |
|------|---------|
| `tf/` | Public rules (`tf_library`, `tf_deploy`, `tf_fmt`) |
| `tf/providers/` | `tf_providers` Bzlmod extension |
| `tf/private/` | Internal rule implementations |
| `tf/private/cmd/runner/` | Go binary: wraps `tofu` at runtime |
| `tf/private/cmd/dupcheck/` | Go binary: duplicate-variable-key check |
| `toolchain/` | OpenTofu binary download + toolchain registration |
| `tests/integration/` | Bazel-in-Bazel negative integration tests |
| `examples/` | End-to-end usage examples |

## Hard invariants

Before submitting a PR, make sure you have not violated these:

1. **No `.terraform.lock.hcl` files** — provider pinning is handled exclusively via `MODULE.bazel` SHA256 maps. Never add, commit, or accept `.terraform.lock.hcl` in any rule's `srcs` or `data`.
2. **Build = validation** — `bazel build` on any `tf_library` or `tf_deploy` target must run `tofu init -backend=false && tofu validate`. Do not bypass or defer this.
3. **Private API** — nothing under `tf/private/` is a supported public API. External users load only from `@rules_tofu//tf:defs.bzl` and `@rules_tofu//tf/providers:extensions.bzl`.
4. **`bazel run` only** — `tf_deploy`'s `.plan`/`.apply`/`.destroy` targets require `BUILD_WORKSPACE_DIRECTORY` to be set; they must be invoked via `bazel run`, never executed directly.

## Running the tests

```sh
# Unit tests (Go):
bazel test //tf/private/cmd/...

# Integration tests (Bazel-in-Bazel, slower):
bazel test //tests/integration/...

# Format check:
bazel test //... --test_tag_filters=fmt
```

## Starlark formatting

All `.bzl` and `BUILD.bazel` files must be formatted with
[buildifier](https://github.com/bazelbuild/buildtools/tree/main/buildifier):

```sh
buildifier -r .
```

CI enforces this; PRs with formatting violations will fail.

## Commit style

- Imperative subject line, ≤ 72 characters.
- Blank line between subject and body.
- Reference GitHub issues/PRs where relevant.

## Branch naming

Use `claude/<short-description>` for AI-assisted branches,
`feat/<short-description>` for feature branches, and
`fix/<short-description>` for bug fixes.

## Opening a PR

1. Fork the repo and create a branch.
2. Make your changes and ensure `bazel build //...` and `bazel test //...` both pass.
3. Run `buildifier -r .` and commit any formatting changes.
4. Open a PR against `main` with a clear description of what changed and why.
