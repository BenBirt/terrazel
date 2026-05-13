"""`terraform_provider` rule.

A `terraform_provider` target wraps the per-platform binaries of a single
OpenTofu/Terraform provider plugin (e.g. `hashicorp/aws` at a given version).

It is intended to be instantiated by the `terraform_providers` module
extension (see `//terraform/providers:extensions.bzl`); end users do not
write `terraform_provider(...)` rules directly. Instead, they declare the
provider in MODULE.bazel and reference the resulting `@<repo>//:provider`
label from a `terraform_library(providers = [...])` or
`terraform_deploy(providers = [...])` attribute.
"""

load(":providers.bzl", "TerraformProviderInfo")

visibility(["public"])

def _terraform_provider_impl(ctx):
    binaries = {}
    for tgt, platform_key in ctx.attr.binaries.items():
        files = tgt.files.to_list()
        if len(files) != 1:
            fail("terraform_provider `{}` binary for {} must be a single file, got {}".format(
                ctx.label,
                platform_key,
                [f.path for f in files],
            ))
        binaries[platform_key] = files[0]

    return [
        DefaultInfo(files = depset(direct = binaries.values())),
        TerraformProviderInfo(
            address = ctx.attr.address,
            version = ctx.attr.version,
            binaries = binaries,
        ),
    ]

terraform_provider = rule(
    implementation = _terraform_provider_impl,
    attrs = {
        "address": attr.string(
            mandatory = True,
            doc = "Canonical provider address, e.g. \"registry.opentofu.org/hashicorp/aws\".",
        ),
        "version": attr.string(
            mandatory = True,
            doc = "Provider version, e.g. \"5.70.0\".",
        ),
        "binaries": attr.label_keyed_string_dict(
            mandatory = True,
            allow_files = True,
            doc = "Map of provider binary file -> `<os>_<arch>` platform key. " +
                  "Each file must already live at the canonical plugin-dir path " +
                  "`<host>/<namespace>/<name>/<version>/<os>_<arch>/<filename>` " +
                  "relative to the work tree's plugin-dir root.",
        ),
    },
    provides = [TerraformProviderInfo],
    doc = "Wraps the per-platform binaries of a single OpenTofu/Terraform provider plugin.",
)
