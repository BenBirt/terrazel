# terrazel

Bazel rules for managing [OpenTofu](https://opentofu.org/) (and, by extension,
Terraform-compatible) configuration as first-class build targets.

`terrazel` exposes two rules, patterned after the archived
[rules_k8s](https://github.com/bazelbuild/rules_k8s):

- `terraform_library` — a reusable bundle of `.tf` files plus its transitive
  `terraform_library` deps. Analogous to `cc_library`. Not directly runnable
  and carries no variable values.
- `terraform_deploy` — a *root* invocation that binds variable values to
  one or more `terraform_library` targets. The macro automatically
  generates two runnable sub-targets: `:foo.plan` and `:foo.apply`.
  Analogous to `cc_binary`.

## Quick start

In your downstream repo's `MODULE.bazel`:

```python
bazel_dep(name = "terrazel", version = "0.1.0")

tofu = use_extension("@terrazel//toolchain:extensions.bzl", "tofu")
# tofu.version(version = "1.8.5")  # optional; defaults to a pinned version
use_repo(tofu, "tofu_toolchains")
```

In a `BUILD.bazel`:

```python
load("@terrazel//terraform:defs.bzl", "terraform_library", "terraform_deploy")

terraform_library(
    name = "network",
    srcs = ["network.tf", "outputs.tf"],
)

terraform_deploy(
    name = "prod",
    deps = [":network"],
    srcs = ["backend.tf"],            # provider + backend config local to this deploy
    vars = {
        "region": "us-east-1",
        "env":    "prod",
    },
)
```

Then:

```sh
bazel run //path/to:prod.plan
bazel run //path/to:prod.apply
```

## How it works

At analysis time `terraform_deploy` materializes every transitive `.tf`
file into a directory tree under `bazel-bin/<pkg>/<name>.work/` via
`ctx.actions.symlink`, preserving each file's workspace-relative path.
It also writes `<name>.work/<pkg>/terrazel.auto.tfvars.json` from
`vars = {...}`.

At runtime the launcher invokes a small Go binary that cd's into the
materialized work tree, runs `tofu init -input=false`, then runs
`tofu plan` (saving `tfplan`) or `tofu apply` (which re-plans first so
it never applies a stale plan).

State is persisted per-deploy at the deploy target's `$(RULEDIR)`, i.e.
`bazel-bin/<package>/<name>.terrazel-state/` (already covered by the
standard `bazel-*` gitignore and wiped by `bazel clean`). If the
deploy declares a `backend "..." {}` block in any of its `.tf` files,
the runner defers to that backend and skips the local-state flags.

## Module source paths

Each `.tf` is materialized at its workspace-relative path, so:

- Same-package files reference each other bare.
- Sibling-package libraries reference each other via relative paths:

```hcl
module "dns" {
  source = "../dns"
}
```

Note: Terraform parses any `source = ` string that does **not** start
with `./` or `../` as a *registry address* (e.g.
`hashicorp/consul/aws`). To reach across the workspace tree, traverse
up to the workspace root with `../..` and back down:

```hcl
module "dns" {
  source = "../../infra/dns"
}
```

## Restrictions

- Only `.tf`, `.tf.json`, `.tftpl`, and `.hcl` files are allowed in
  `srcs`. `.tfvars` and `.tfvars.json` are intentionally rejected —
  variable values must come through `terraform_deploy(vars = {...})`,
  not via files committed in libraries.
- Files from external Bazel modules cannot be included in a deploy: the
  runner cannot give them a sensible workspace-relative path that
  Terraform's local-module addressing can reach.

## TODOs / known gaps

- Build OpenTofu from source via rules_go (currently: download pinned
  binary).
- Additional sub-commands: `.destroy`, `.validate`, `.fmt`, `.import`,
  `.console`.
- Hermetic provider plugin vendoring via `-plugin-dir`.
- Treat `.terraform.lock.hcl` as a first-class input.
- Windows host support (downloads work; launcher script is bash-only).
