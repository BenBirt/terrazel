# Notes for Claude Code

Public docs are in `README.md`. This file is the agent-specific overlay:
sandbox bootstrap, hard invariants, repo conventions. Don't add general
documentation here.

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

Then `bazel shutdown` so the new startup flags take effect.

`bazel build //...` still fails in the sandbox because `rules_go` fetches
`https://go.dev/dl/?mode=json` (proxy 403). CI has open network and is
unaffected. Locally, exercise the parts that don't pull in the runner:

- `bazel build //examples/hello:hello //examples/aws:s3_bucket //examples/gcp:storage_bucket`
- `bazel build //examples/hello:greet_lib //examples/aws:s3_bucket_lib //examples/gcp:storage_bucket_lib`
- `bazel test //examples/...:all` (fmt_check tests only)
- Runner unit tests outside Bazel: copy `main.go` + `main_test.go` to a
  scratch dir with a trivial `go.mod` and `go test ./...` (the canonical
  Bazel run is `bazel test //terraform/private/cmd/runner:runner_test`,
  which needs network).

## Invariants — don't regress these

- No `.terraform.lock.hcl` is generated, shipped, or accepted in
  `srcs`/`data`. `MODULE.bazel.lock` + the per-platform `sha256` on each
  `terraform_providers.provider(...)` tag is the sole pinning layer.
  `init_action.bzl` fails loudly if a lock file slips into the work tree;
  don't relax that check.
- `terraform_library` validates inline — the validate stamp goes in
  `DefaultInfo.files` and `bazel build :foo_lib` is the validation
  contract. Don't add a `:foo.validate` sub-target or a `validate_test`
  macro flag. `bazel query 'kind("test", //...)'` should only return
  `:foo.fmt_check`.
- `_ALLOWED_EXTS` in `library.bzl` / `deploy.bzl` includes `.hcl`, which
  technically permits a user to list `.terraform.lock.hcl` in `srcs`.
  The runtime rejection in `init_action.bzl` is the backstop — keep it.
- The build-time validate action and the runtime runner both pick the
  **exec-platform** provider binary. Cross-compilation to a different
  target platform is out of scope and isn't worth designing around
  speculatively.
- The runner expects `--plugin-dir` to be required, and `os.MkdirAll`s
  it on first use so the zero-providers case still presents tofu with
  an extant directory. Don't make the flag optional.
- Shared materialization helpers live in `terraform/private/work_tree.bzl`
  (`materialize`, `materialize_plugin_tree`, `work_tree_root`,
  `PLUGIN_DIR_RELPATH`). Both `library.bzl` and `deploy.bzl` should go
  through them; don't reintroduce inline copies.

## Branch / PR conventions

Development branch (`claude/...`); push there, not `main`. Don't open a
PR unless asked. Don't include the model identifier (`claude-opus-4-7`
etc.) in any commit message, PR title, or code artifact.
