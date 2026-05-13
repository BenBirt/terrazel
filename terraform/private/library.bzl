"""`terraform_library` rule: a reusable bundle of OpenTofu/Terraform
configuration files, plus transitive `terraform_library` deps. Not
directly runnable; bind variable values and produce runnable
`.plan`/`.apply` targets with `terraform_deploy`.
"""

load(":deploy.bzl", _terraform_deploy_rule = "terraform_deploy_rule")
load(":fmt.bzl", _tf_fmt = "tf_fmt", _tf_fmt_check = "tf_fmt_check_test")
load(":providers.bzl", "TerraformLibraryInfo", "TerraformProviderInfo")

# Only structural Terraform inputs are allowed in srcs. Variable values
# come from `terraform_deploy(vars = {...})`; allowing `.tfvars[.json]`
# here would create ambiguous precedence with the deploy-emitted
# `terrazel.auto.tfvars.json` and lets a reusable module declare values
# it has no business owning.
_ALLOWED_EXTS = [".tf", ".tf.json", ".tftpl", ".hcl"]

def _terraform_library_impl(ctx):
    direct = [
        struct(path = f.short_path, file = f)
        for f in ctx.files.srcs + ctx.files.data
    ]
    transitive = [
        d[TerraformLibraryInfo].transitive_files
        for d in ctx.attr.deps
    ]
    files = depset(direct = direct, transitive = transitive)

    direct_providers = [p[TerraformProviderInfo] for p in ctx.attr.providers]
    transitive_providers = [
        d[TerraformLibraryInfo].providers
        for d in ctx.attr.deps
    ]
    providers_depset = depset(direct = direct_providers, transitive = transitive_providers)

    return [
        DefaultInfo(files = depset(direct = ctx.files.srcs + ctx.files.data)),
        TerraformLibraryInfo(
            transitive_files = files,
            providers = providers_depset,
        ),
    ]

_terraform_library = rule(
    implementation = _terraform_library_impl,
    attrs = {
        "srcs": attr.label_list(
            allow_files = _ALLOWED_EXTS,
            doc = "Source files belonging to this library (.tf, .tf.json, .tftpl, .hcl).",
        ),
        "deps": attr.label_list(
            providers = [TerraformLibraryInfo],
            doc = "Other `terraform_library` targets whose files this library composes with.",
        ),
        "data": attr.label_list(
            allow_files = True,
            doc = "Arbitrary files to include alongside the Terraform sources in the work tree. " +
                  "Use to expose files for `file()` calls in Terraform configs.",
        ),
        "providers": attr.label_list(
            providers = [TerraformProviderInfo],
            doc = "Vendored OpenTofu/Terraform providers this library references. " +
                  "Declare each provider once in MODULE.bazel via the `terraform_providers` " +
                  "extension and pass `@<repo>//:provider` here. Propagates transitively to " +
                  "any `terraform_deploy` that pulls this library in.",
        ),
    },
    doc = """Bundles a set of OpenTofu/Terraform configuration files for reuse.

A `terraform_library` carries no variable values and is not directly
runnable. Use `terraform_deploy` to bind variable values and produce
`:foo.plan` / `:foo.apply` runnable sub-targets.

At runtime, every file in `srcs` is materialized at its
workspace-relative path, so:
  - Same-package files reference each other bare.
  - Cross-package deps reference each other with
    `source = "../other_lib"`.

Note: Terraform parses any `source` not starting with `./` or `../` as
a registry address, so to reach a top-level workspace directory from a
deeply nested module, traverse with `../../...`.
""",
)

def terraform_library(name, srcs = None, deps = None, data = None, providers = None, fmt_test = True, **kwargs):
    """A reusable bundle of OpenTofu/Terraform configuration files.

    Always generates:
      - `:<name>`          — the library target (carries TerraformLibraryInfo).
      - `:<name>.validate` — non-test build target. Building it materializes a
                             work tree containing the library's transitive files
                             and runs `tofu init -backend=false && tofu validate`
                             against the library's package. Use
                             `bazel build :<name>.validate` (no longer `bazel test`).
      - `:<name>.fmt`      — `bazel run` to reformat .tf files in-place.

    When enabled (default True):
      - `:<name>.fmt_check`  — `bazel test` that fails if files are not formatted (fmt_test=True).

    Args:
      name: target name.
      srcs: source .tf/.tf.json/.tftpl/.hcl files.
      deps: other `terraform_library` targets to compose with.
      data: arbitrary files to include in the work tree for `file()` calls.
      providers: `terraform_provider` targets (typically `@<repo>//:provider` exposed
          by `terraform_providers.provider(...)` in MODULE.bazel) that this library
          references in `required_providers`. Propagates transitively to any
          `terraform_deploy` consuming this library.
      fmt_test: whether to emit a `:<name>.fmt_check` test target (default True).
      **kwargs: forwarded to the underlying rule (visibility, tags, testonly).
    """
    common_kwargs = {}
    for forwarded in ("visibility", "tags", "testonly"):
        if forwarded in kwargs:
            common_kwargs[forwarded] = kwargs.pop(forwarded)

    _terraform_library(
        name = name,
        srcs = srcs or [],
        deps = deps or [],
        data = data or [],
        providers = providers or [],
        **dict(common_kwargs, **kwargs)
    )

    # Build-time validation: emit a thin deploy rooted at this package so its
    # build action runs `tofu init -backend=false && tofu validate` against
    # the library's transitive files.
    _terraform_deploy_rule(
        name = name + ".validate",
        srcs = [],
        deps = [":" + name],
        vars = {},
        var_files = [],
        data = [],
        providers = [],
        **common_kwargs
    )

    _tf_fmt(name = name + ".fmt")
    if fmt_test:
        _tf_fmt_check(
            name = name + ".fmt_check",
            srcs = srcs or [],
        )
