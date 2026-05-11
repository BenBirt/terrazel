# terrazel

Bazel rules for managing [OpenTofu](https://opentofu.org/) (and, by extension,
Terraform-compatible) configuration as first-class build targets.

`terrazel` exposes two rules, patterned after the archived
[rules_k8s](https://github.com/bazelbuild/rules_k8s):

- `terraform_library` — a reusable bundle of `.tf` files plus its transitive
  `terraform_library` deps. Analogous to `cc_library`. Not directly runnable.
- `terraform_deploy` — a *root* invocation that binds variable values to one
  or more `terraform_library` targets. The macro automatically generates two
  runnable sub-targets: `:foo.plan` and `:foo.apply`. Analogous to
  `cc_binary`.

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

## File layout at runtime

Each input `.tf` file is materialized into a scratch working tree at its
**workspace-relative path**. Same-package files reference each other bare;
sibling-package deps reference each other via relative paths. For example,
to depend on a library at `//infra/dns:dns` from a config at
`//infra/networking:network`, write:

```hcl
module "dns" {
  source = "../dns"
}
```

### Module source path constraint

Terraform/OpenTofu parses any `source = ` string that does **not** start with
`./` or `../` as a *registry address* (e.g. `hashicorp/consul/aws`).
So `source = "infra/dns"` will not work as a local-path reference. To
reach across the workspace tree, traverse up to the workspace root with
`../..` and back down:

```hcl
module "dns" {
  source = "../../infra/dns"
}
```

## What runs when you `bazel run :foo.plan`

1. Scratch working directory is created.
2. All transitive `.tf` files from `deps` and `srcs` are materialized at
   their workspace-relative paths.
3. `terrazel.auto.tfvars.json` is rendered from the deploy's `vars` attr
   and placed in the deploy's package directory.
4. `tofu init -input=false` runs.
5. `tofu plan -input=false -out=tfplan` runs.

`bazel run :foo.apply` re-plans, then `tofu apply tfplan` applies the
freshly captured plan artifact.

State is persisted at
`$BUILD_WORKSPACE_DIRECTORY/.terrazel/state/<label_hash>/` for the default
local backend; use a remote backend in your `.tf` for anything beyond
single-developer experiments.

## TODOs / known gaps

- Build OpenTofu from source via rules_go (currently: download pinned binary).
- Additional sub-commands: `.destroy`, `.validate`, `.fmt`, `.import`,
  `.console`.
- `tfvars` file inputs (only `vars = {...}` dict supported today).
- Hermetic provider plugin vendoring via `-plugin-dir`.
- Windows host support (downloads work; runner script is bash-only).
