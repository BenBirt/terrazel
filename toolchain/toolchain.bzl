"""OpenTofu toolchain definition.

A `terraform_deploy.<plan|apply>` runner resolves the active toolchain via
`ctx.toolchains[TOOLCHAIN_TYPE]` and reads `TofuInfo.binary` to find the
`tofu` executable to invoke at runtime.
"""

TOOLCHAIN_TYPE = "@terrazel//toolchain:toolchain_type"

TofuInfo = provider(
    doc = "Information about a resolved OpenTofu binary.",
    fields = {
        "binary": "File: the `tofu` executable.",
        "version": "string: the OpenTofu version, e.g. \"1.8.5\".",
    },
)

def _opentofu_toolchain_impl(ctx):
    tofu_info = TofuInfo(
        binary = ctx.file.binary,
        version = ctx.attr.version,
    )
    return [
        platform_common.ToolchainInfo(tofu = tofu_info),
    ]

opentofu_toolchain = rule(
    implementation = _opentofu_toolchain_impl,
    attrs = {
        "binary": attr.label(
            allow_single_file = True,
            mandatory = True,
            doc = "The `tofu` executable.",
        ),
        "version": attr.string(
            mandatory = True,
            doc = "OpenTofu version this toolchain wraps, e.g. \"1.8.5\".",
        ),
    },
    doc = "Bundles an OpenTofu binary for use by terrazel rules.",
)
