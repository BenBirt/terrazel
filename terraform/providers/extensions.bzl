"""Bzlmod module extension that downloads pinned OpenTofu/Terraform provider
plugins and exposes them as `terraform_provider` build targets, ready to be
referenced from `terraform_library(providers = [...])` and
`terraform_deploy(providers = [...])`.

Downstream usage in MODULE.bazel:

    terraform_providers = use_extension(
        "@terrazel//terraform/providers:extensions.bzl",
        "terraform_providers",
    )
    terraform_providers.provider(
        name = "tf_hashicorp_null",
        source = "hashicorp/null",
        version = "3.2.2",
        sha256 = {
            "linux_amd64":   "...",
            "linux_arm64":   "...",
            "darwin_amd64":  "...",
            "darwin_arm64":  "...",
            "windows_amd64": "...",
        },
    )
    use_repo(terraform_providers, "tf_hashicorp_null")

The target `@tf_hashicorp_null//:provider` then carries
`TerraformProviderInfo` and is passed to `terraform_library`/`terraform_deploy`
via their `providers` attribute. Bazel's MODULE.bazel.lock pins the resolved
SHAs, so the provider tree is reproducible and is fetched once per workspace.

Currently only providers hosted on `releases.hashicorp.com` (i.e. published by
the `hashicorp/` namespace) are supported. Other registries require resolving
the download URL via `registry.terraform.io`'s download protocol; that can be
added later behind a `url_template` attribute.
"""

# Default plugin-dir host segment. OpenTofu canonicalizes `source = "<ns>/<name>"`
# in `required_providers` to `registry.opentofu.org/<ns>/<name>` and looks for
# plugins under `<plugin-dir>/registry.opentofu.org/...`. terrazel uses OpenTofu,
# so the vendored plugin tree mirrors that layout.
_DEFAULT_HOST = "registry.opentofu.org"

# (os/arch -> exe suffix). Keys must match the `sha256` map's platform keys.
_EXE_SUFFIX = {
    "linux_amd64": "",
    "linux_arm64": "",
    "linux_386": "",
    "darwin_amd64": "",
    "darwin_arm64": "",
    "windows_amd64": ".exe",
    "windows_386": ".exe",
}

def _split_source(source):
    parts = source.split("/")
    if len(parts) != 2 or not parts[0] or not parts[1]:
        fail("provider `source` must be \"<namespace>/<name>\", got {}".format(repr(source)))
    return parts[0], parts[1]

def _split_platform_key(platform_key):
    parts = platform_key.split("_", 1)
    if len(parts) != 2 or not parts[0] or not parts[1]:
        fail("provider platform key must be \"<os>_<arch>\", got {}".format(repr(platform_key)))
    return parts[0], parts[1]

def _format_binaries_dict(entries):
    """Format `entries` as a Starlark dict literal for the generated BUILD.

    `entries` is a list of `(binary_path, platform_key)` tuples. The dict
    is keyed by file label (so `label_keyed_string_dict` resolves each to a
    File) and valued by platform key string.
    """
    lines = ["    binaries = {"]
    for binary_path, platform_key in entries:
        lines.append("        \"{}\": \"{}\",".format(binary_path, platform_key))
    lines.append("    },")
    return "\n".join(lines)

def _terraform_provider_download_impl(repository_ctx):
    source = repository_ctx.attr.source
    version = repository_ctx.attr.version
    sha256 = repository_ctx.attr.sha256
    namespace, name = _split_source(source)

    if namespace != "hashicorp":
        fail(
            ("terraform_providers.provider `{}` has source `{}`: only providers from " +
             "the `hashicorp/` namespace (served via releases.hashicorp.com) are " +
             "currently supported. Support for other registries can be added by " +
             "extending the extension with a `url_template` attribute.").format(
                repository_ctx.attr.name,
                source,
            ),
        )

    if not sha256:
        fail("terraform_providers.provider `{}` must declare at least one platform in `sha256`".format(
            repository_ctx.attr.name,
        ))

    address = "{}/{}/{}".format(_DEFAULT_HOST, namespace, name)
    entries = []
    for platform_key in sorted(sha256.keys()):
        if platform_key not in _EXE_SUFFIX:
            fail("terraform_providers.provider `{}` declares unknown platform `{}`. Known: {}".format(
                repository_ctx.attr.name,
                platform_key,
                sorted(_EXE_SUFFIX.keys()),
            ))
        os_name, arch = _split_platform_key(platform_key)
        extract_dir = "plugins/{host}/{ns}/{name}/{version}/{platform}".format(
            host = _DEFAULT_HOST,
            ns = namespace,
            name = name,
            version = version,
            platform = platform_key,
        )
        url = ("https://releases.hashicorp.com/terraform-provider-{name}/{version}/" +
               "terraform-provider-{name}_{version}_{os}_{arch}.zip").format(
            name = name,
            version = version,
            os = os_name,
            arch = arch,
        )

        repository_ctx.download_and_extract(
            url = url,
            sha256 = sha256[platform_key],
            type = "zip",
            output = extract_dir,
        )

        # The zip contains the provider binary (typically
        # `terraform-provider-<name>_v<version>{_x5}{.exe}`) plus, on some
        # releases, a LICENSE file. Pick the file whose name starts with the
        # canonical prefix so we don't accidentally grab the LICENSE.
        expected_prefix = "terraform-provider-{}_v{}".format(name, version)
        exe_suffix = _EXE_SUFFIX[platform_key]
        binary_filename = None
        for child in repository_ctx.path(extract_dir).readdir():
            basename = child.basename
            if basename.startswith(expected_prefix) and basename.endswith(exe_suffix):
                if binary_filename != None:
                    fail(("Ambiguous provider binary for {}@{} on {}: both `{}` and " +
                          "`{}` match `{}*{}`. Refusing to guess.").format(
                        source,
                        version,
                        platform_key,
                        binary_filename,
                        basename,
                        expected_prefix,
                        exe_suffix,
                    ))
                binary_filename = basename
        if binary_filename == None:
            fail("No file matching `{}*{}` found in {} after extracting {}".format(
                expected_prefix,
                exe_suffix,
                extract_dir,
                url,
            ))

        entries.append(("{}/{}".format(extract_dir, binary_filename), platform_key))

    # Extracted binaries live in nested directories; Bazel doesn't auto-expose
    # them as source labels, so the `label_keyed_string_dict` on
    # `terraform_provider` couldn't resolve them without an explicit
    # `exports_files()`.
    exports_lines = ["exports_files(["]
    for binary_path, _ in entries:
        exports_lines.append("    \"{}\",".format(binary_path))
    exports_lines.append("])")

    repository_ctx.file("WORKSPACE", "")
    repository_ctx.file(
        "BUILD.bazel",
        content = """\
load("@terrazel//terraform/private:provider.bzl", "terraform_provider")

package(default_visibility = ["//visibility:public"])

{exports}

terraform_provider(
    name = "provider",
    address = "{address}",
    version = "{version}",
{binaries}
)
""".format(
            exports = "\n".join(exports_lines),
            address = address,
            version = version,
            binaries = _format_binaries_dict(entries),
        ),
        executable = False,
    )

_terraform_provider_download = repository_rule(
    implementation = _terraform_provider_download_impl,
    attrs = {
        "source": attr.string(mandatory = True),
        "version": attr.string(mandatory = True),
        "sha256": attr.string_dict(mandatory = True),
    },
)

_provider_tag = tag_class(
    attrs = {
        "name": attr.string(
            mandatory = True,
            doc = "Bazel repo name to expose (used in MODULE.bazel's `use_repo(...)`).",
        ),
        "source": attr.string(
            mandatory = True,
            doc = "Provider source in `<namespace>/<name>` form, e.g. \"hashicorp/null\".",
        ),
        "version": attr.string(
            mandatory = True,
            doc = "Provider version to pin, e.g. \"3.2.2\".",
        ),
        "sha256": attr.string_dict(
            mandatory = True,
            doc = "Per-platform SHA256 of the provider zip. Keys are `<os>_<arch>` " +
                  "(e.g. `linux_amd64`). Declare every platform you intend to run on; " +
                  "build/runtime fail at deploy time if the exec platform is missing.",
        ),
    },
)

def _terraform_providers_extension_impl(module_ctx):
    seen = {}
    for mod in module_ctx.modules:
        for tag in mod.tags.provider:
            if tag.name in seen:
                fail("Duplicate terraform_providers.provider name `{}` (declared in modules `{}` and `{}`)".format(
                    tag.name,
                    seen[tag.name],
                    mod.name,
                ))
            seen[tag.name] = mod.name
            _terraform_provider_download(
                name = tag.name,
                source = tag.source,
                version = tag.version,
                sha256 = tag.sha256,
            )

terraform_providers = module_extension(
    implementation = _terraform_providers_extension_impl,
    tag_classes = {"provider": _provider_tag},
)
