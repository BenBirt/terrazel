"""`tf_provider` rule.

A `tf_provider` target wraps the per-platform binaries of a single
OpenTofu/Terraform provider plugin (e.g. `hashicorp/aws` at a given version).

It is intended to be instantiated by the `tf_providers` module
extension (see `//tf/providers:extensions.bzl`); end users do not
write `tf_provider(...)` rules directly. Instead, they declare the
provider in MODULE.bazel and reference the resulting `@<repo>//:provider`
label from a `tf_library(providers = [...])` or
`tf_deploy(providers = [...])` attribute.
"""

load(":providers.bzl", "TfProviderInfo")

visibility(["public"])

def _tf_provider_impl(ctx):
    binaries = {}
    for tgt, platform_key in ctx.attr.binaries.items():
        files = tgt.files.to_list()
        if len(files) != 1:
            fail("tf_provider `{}` binary for {} must be a single file, got {}".format(
                ctx.label,
                platform_key,
                [f.path for f in files],
            ))
        binaries[platform_key] = files[0]

    return [
        DefaultInfo(files = depset(direct = binaries.values())),
        TfProviderInfo(
            address = ctx.attr.address,
            version = ctx.attr.version,
            binaries = binaries,
        ),
    ]

tf_provider = rule(
    implementation = _tf_provider_impl,
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
    provides = [TfProviderInfo],
    doc = "Wraps the per-platform binaries of a single OpenTofu/Terraform provider plugin.",
)
