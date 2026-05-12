"""`terraform_library` rule: a reusable bundle of OpenTofu/Terraform
configuration files, plus transitive `terraform_library` deps. Not
directly runnable; bind variable values and produce runnable
`.plan`/`.apply` targets with `terraform_deploy`.
"""

load(":providers.bzl", "TerraformLibraryInfo")

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

    return [
        DefaultInfo(files = depset(direct = ctx.files.srcs + ctx.files.data)),
        TerraformLibraryInfo(transitive_files = files),
    ]

terraform_library = rule(
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
