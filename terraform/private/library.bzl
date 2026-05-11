"""`terraform_library` rule: a reusable bundle of .tf files (and transitive
`terraform_library` deps). Not directly runnable.
"""

load(":providers.bzl", "TerraformLibraryInfo")

def _terraform_library_impl(ctx):
    direct = [
        struct(path = f.short_path, file = f)
        for f in ctx.files.srcs
    ]
    transitive = [
        d[TerraformLibraryInfo].transitive_files
        for d in ctx.attr.deps
    ]
    files = depset(direct = direct, transitive = transitive)

    return [
        DefaultInfo(files = depset(direct = ctx.files.srcs)),
        TerraformLibraryInfo(transitive_files = files),
    ]

terraform_library = rule(
    implementation = _terraform_library_impl,
    attrs = {
        "srcs": attr.label_list(
            allow_files = [".tf", ".tf.json", ".tfvars", ".tfvars.json", ".tftpl", ".hcl"],
            doc = "Source files belonging to this library.",
        ),
        "deps": attr.label_list(
            providers = [TerraformLibraryInfo],
            doc = "Other `terraform_library` targets whose files this library composes with.",
        ),
    },
    doc = """Bundles a set of OpenTofu/Terraform configuration files for reuse.

A `terraform_library` carries no variable values and is not directly runnable.
Use `terraform_deploy` to bind variable values and produce `:foo.plan` /
`:foo.apply` runnable sub-targets.

At runtime, every file in `srcs` is materialized at its workspace-relative
path, so:
  - Same-package files reference each other bare.
  - Cross-package deps reference each other with `source = "../other_lib"`.

Note: Terraform parses any `source` not starting with `./` or `../` as a
registry address. To reach a top-level workspace directory from a deeply
nested module, traverse with `../../...`.
""",
)
