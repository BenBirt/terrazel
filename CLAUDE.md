# terrazel — Claude Code notes

Bazel rules for OpenTofu/Terraform. The public surface is in
`terraform/defs.bzl`: `terraform_library` (reusable bundle, no var values),
`terraform_deploy` (root invocation that binds vars and emits
`:foo.{plan,apply,destroy,fmt}` sub-targets), plus a `terraform_providers`
Bzlmod extension for vendoring provider plugin binaries.

## Repo layout

- `terraform/defs.bzl` — public API. Re-exports `terraform_library` and
  `terraform_deploy`. Downstreams `load("@terrazel//terraform:defs.bzl", ...)`.
- `terraform/private/` — rule implementations. Touch these for behavioral
  changes; never re-export anything from here at the package boundary.
- `terraform/private/cmd/runner/` — small Go runner that wraps `tofu`
  at `bazel run` time. All configuration is passed via explicit flags from
  the generated bash launcher (`runner.bzl`); there are no env-driven knobs.
- `terraform/providers/extensions.bzl` — `terraform_providers` Bzlmod
  extension (downloads per-platform provider zips, exposes
  `@<repo>//:provider` carrying `TerraformProviderInfo`).
- `toolchain/` — the OpenTofu binary toolchain (download + register).
- `examples/` — end-to-end examples: `hello/` (no providers),
  `aws/` (real `hashicorp/aws`), `gcp/` (real `hashicorp/google`).

## Architecture pointers

- Providers (`TerraformProviderInfo`) are vendored per-platform via
  `terraform_providers.provider(...)` in MODULE.bazel; the deploy rule
  symlinks the exec-platform binary into the work tree at the canonical
  `<host>/<ns>/<name>/<version>/<os>_<arch>/` layout under
  `.terrazel-plugins/`. OpenTofu's default host is `registry.opentofu.org`;
  the extension hardcodes that as the layout's host segment.
- `tofu init` runs **twice** with `-plugin-dir`:
  1. At `bazel build` time, with `-backend=false`, inside a copy of the work
     tree under `$TMPDIR` (so sandbox read-only enforcement on input mounts
     never matters). Followed by `tofu validate`. Drives the deploy rule's
     `TofuValidate` action and the validate stamp output.
  2. At `bazel run` time, in the runfiles work tree, with the live backend.
- No `.terraform.lock.hcl` is generated, shipped, or accepted in
  `srcs`/`data`. `MODULE.bazel.lock` + the per-platform `sha256` recorded
  on each `terraform_providers.provider(...)` tag is the sole pinning
  layer. `init_action.bzl` rejects any lock file that slips into the
  work tree.
- `terraform_library` rule validates inline: its build action materializes
  the library's transitive files into a work tree, symlinks the vendored
  provider binaries, and runs `tofu init -backend=false && tofu validate`.
  The validate stamp lives in `DefaultInfo.files`, so `bazel build :foo_lib`
  fails when validation fails. There is no separate `.validate` sub-target
  on libraries (only `:<name>.fmt_check` shows up under
  `bazel query 'kind("test", //...)'`).

## Build / test in the Anthropic sandbox

`bcr.bazel.build` is not in the proxy allowlist. Before the first build,
write `.bazelrc.local` (gitignored; the committed `.bazelrc` already
`try-import`s it) using the llmbox bootstrap script. Run once per session:

```python
import os

proxy = os.environ.get("GLOBAL_AGENT_HTTP_PROXY", "")

lines = [
    "# Auto-generated — do not commit (gitignored)",
    "",
    "common --registry=https://raw.githubusercontent.com/bazelbuild/bazel-central-registry/main",
    "",
    "startup --host_jvm_args=-Djavax.net.ssl.trustStore=/etc/ssl/certs/java/cacerts",
    "startup --host_jvm_args=-Djavax.net.ssl.trustStorePassword=changeit",
]

if proxy:
    no_scheme = proxy[len("http://"):]
    userinfo, hostport = no_scheme.rsplit("@", 1)
    host, port = hostport.rsplit(":", 1)
    user, password = userinfo.split(":", 1)
    lines += [
        "",
        f"startup --host_jvm_args=-Dhttps.proxyHost={host}",
        f"startup --host_jvm_args=-Dhttps.proxyPort={port}",
        f"startup --host_jvm_args=-Dhttps.proxyUser={user}",
        f"startup --host_jvm_args=-Dhttps.proxyPassword={password}",
        f"startup --host_jvm_args=-Dhttp.proxyHost={host}",
        f"startup --host_jvm_args=-Dhttp.proxyPort={port}",
        f"startup --host_jvm_args=-Dhttp.proxyUser={user}",
        f"startup --host_jvm_args=-Dhttp.proxyPassword={password}",
        "startup --host_jvm_args=-Dhttp.nonProxyHosts=localhost|127.*|[::1]",
        "",
        "startup --host_jvm_args=-Djdk.http.auth.tunneling.disabledSchemes=",
        "startup --host_jvm_args=-Djdk.http.auth.proxying.disabledSchemes=",
    ]

with open(".bazelrc.local", "w") as f:
    f.write("\n".join(lines) + "\n")
```

After writing it, `bazel shutdown` so the new startup flags take effect on
the next invocation.

**Known sandbox limitation:** `bazel build //...` will still fail in the
sandbox because the runner's `rules_go` Go SDK download fetches
`https://go.dev/dl/?mode=json`, which the proxy blocks (`403
host_not_allowed`). CI has open network and is unaffected. Locally,
exercise the parts that don't pull in the runner:

- `bazel build //examples/hello:hello //examples/aws:s3_bucket //examples/gcp:storage_bucket`
  (deploy targets — each runs `tofu init -backend=false && tofu validate`).
- `bazel build //examples/hello:greet_lib //examples/aws:s3_bucket_lib //examples/gcp:storage_bucket_lib`
  (library targets validate inline too, no separate `.validate`).
- `bazel test //examples/...:all` (fmt_check tests only)
- Go runner unit tests run cleanly outside Bazel: `cd terraform/private/cmd/runner && go test ./...`
  with a self-contained `go.mod` (the canonical Bazel run is
  `bazel test //terraform/private/cmd/runner:runner_test`, which requires
  network).

## Provider examples

`examples/aws/` and `examples/gcp/` declare real providers
(`hashicorp/aws@5.70.0`, `hashicorp/google@5.45.0`) via the
`terraform_providers` extension in MODULE.bazel. Sha256s for five
platforms are pinned so `tofu init -plugin-dir=...` is fully offline.
Use these as templates when wiring a new provider:

1. Run `curl -sL https://releases.hashicorp.com/terraform-provider-<name>/<v>/terraform-provider-<name>_<v>_SHA256SUMS`
   and pull out the per-platform hashes.
2. Add a `terraform_providers.provider(...)` tag in MODULE.bazel and a
   `use_repo` for the new `@tf_*` name.
3. Add `providers = ["@<repo>//:provider"]` to your `terraform_library`.

Only providers in the `hashicorp/` namespace are supported today (they
ship via `releases.hashicorp.com`). Adding registries that require the
download protocol is a TODO.

## Things to remember when editing

- The build-time TofuValidate action and the runtime runner both pick the
  **exec-platform** provider binary; cross-compilation to a different
  target platform is not supported and isn't worth designing around
  speculatively.
- `_ALLOWED_EXTS` in `library.bzl` / `deploy.bzl` includes `.hcl`, which
  means a user could in principle list `.terraform.lock.hcl` in `srcs`.
  `init_action.bzl` fails loudly if one shows up in the materialized tree;
  don't relax that check.
- Don't add a `validate_test` flag back to the macros, and don't add a
  `:foo.validate` sub-target. Both library and deploy rules attach the
  validate stamp directly to their `DefaultInfo.files` — `bazel build :foo`
  is the validate contract.
- The runner expects `--plugin-dir` to be required, and `os.MkdirAll`s it
  on first use so the zero-providers case still presents tofu with an
  extant directory.

## Branch / PR conventions

This session runs on a development branch (`claude/...`); push there, not
`main`. Don't open a PR unless asked. Don't include the model identifier
(`claude-opus-4-7` etc.) in any commit message, PR title, or code
artifact.
