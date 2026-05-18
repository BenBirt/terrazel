"""Work tree materialization shared between `tf_library` and
`tf_deploy`.

A "work tree" is the directory under `bazel-bin/<pkg>/<name>.work/` into
which a rule symlinks every transitive `.tf`/data file at its
workspace-relative path, plus (for deploys) a generated
`rules_tofu.auto.tfvars.json`, plus the vendored provider plugin tree under
`.rules_tofu-plugins/`. `tofu init -plugin-dir=<plugin_tree>` then sees a
self-contained tree that mirrors a real Terraform working directory.

`materialize(...)` handles the .tf/data half (with optional tfvars).
`materialize_plugin_tree(...)` handles the per-provider symlinks.
"""

load("//toolchain:toolchain.bzl", "TOOLCHAIN_TYPE")

# Path within a work tree at which we materialize provider plugin binaries.
# Layout under this root follows Terraform's standard plugin-dir convention:
# `<host>/<namespace>/<name>/<version>/<os>_<arch>/<binary>`.
PLUGIN_DIR_RELPATH = ".rules_tofu-plugins"

def materialize(ctx, entries, tfvars_content = None):
    """Materialize a work tree under `<pkg>/<name>.work/`.

    For each `struct(path, file)` in `entries`, declares `<name>.work/<path>`
    and symlinks it to `file`. If `tfvars_content` is non-None, also writes
    `<name>.work/<package>/rules_tofu.auto.tfvars.json` with that content.

    Args:
      ctx: rule ctx.
      entries: list[struct(path, file)] — workspace-relative paths + Files.
      tfvars_content: optional string. Pass None for library work trees
          (which carry no variable values).

    Returns:
      (list[File], string): the declared outputs and the workspace-relative
      package dir (i.e. `ctx.label.package`) that the caller will pass to
      `tf_init_validate` as the cwd.
    """
    work_prefix = ctx.label.name + ".work"
    outputs = []
    seen = {}
    for entry in entries:
        path = entry.path
        if path.startswith("../"):
            fail(
                "`{}` would include `{}` from an external Bazel module. ".format(
                    ctx.label,
                    entry.file.path,
                ) + "Terraform has no addressing scheme for files outside the workspace " +
                "root, so this is not supported. Bring the file in-workspace (e.g. via " +
                "a `genrule` or a local copy) and depend on that instead.",
            )
        if path in seen:
            other = seen[path]
            if other != entry.file:
                fail(
                    "File collision at workspace path `{}` between `{}` and `{}`. ".format(
                        path,
                        other.path,
                        entry.file.path,
                    ) + "Two libraries are contributing different content at the same path.",
                )
            continue
        seen[path] = entry.file
        out = ctx.actions.declare_file(work_prefix + "/" + path)
        ctx.actions.symlink(output = out, target_file = entry.file)
        outputs.append(out)

    if tfvars_content != None:
        tfvars_path = work_prefix + "/" + ctx.label.package + "/rules_tofu.auto.tfvars.json"
        if tfvars_path[len(work_prefix) + 1:] in seen:
            fail(
                "`{}` collides with the generated rules_tofu.auto.tfvars.json. ".format(
                    seen[tfvars_path[len(work_prefix) + 1:]].path,
                ) + "Rename or remove that file; deploy `vars` is the sole producer of tfvars.",
            )
        tfvars_file = ctx.actions.declare_file(tfvars_path)
        ctx.actions.write(output = tfvars_file, content = tfvars_content)
        outputs.append(tfvars_file)

    return outputs

def materialize_plugin_tree(ctx, providers_depset):
    """Symlink provider binaries into the work tree's plugin directory.

    Creates one symlink per unique `(address, version)` using Terraform's
    canonical layout.

    Args:
      ctx: rule ctx. Must list `TOOLCHAIN_TYPE` in `toolchains` so
          `tofu.platform_key` is available.
      providers_depset: depset[TfProviderInfo].

    Returns:
      list[File]: declared symlink outputs (possibly empty).

    Fails if two providers share an address but differ in version, or if a
    declared provider has no binary for the exec platform.
    """
    tofu = ctx.toolchains[TOOLCHAIN_TYPE].tofu
    platform_key = tofu.platform_key
    work_prefix = ctx.label.name + ".work"

    seen_versions = {}
    outputs = []
    for prov in providers_depset.to_list():
        prior = seen_versions.get(prov.address)
        if prior != None:
            if prior != prov.version:
                fail(
                    ("`{label}` has conflicting versions for provider `{addr}`: " +
                     "`{a}` vs `{b}`. Pick one in MODULE.bazel.").format(
                        label = ctx.label,
                        addr = prov.address,
                        a = prior,
                        b = prov.version,
                    ),
                )
            continue
        seen_versions[prov.address] = prov.version

        binary = prov.binaries.get(platform_key)
        if binary == None:
            fail(
                ("`{label}` requires provider `{addr}@{ver}` for exec platform " +
                 "`{plat}`, but the provider was declared without a `{plat}` entry " +
                 "in its `sha256` map. Add it in MODULE.bazel.").format(
                    label = ctx.label,
                    addr = prov.address,
                    ver = prov.version,
                    plat = platform_key,
                ),
            )

        parts = prov.address.split("/")
        if len(parts) != 3:
            fail("invalid provider address `{}` (expected `<host>/<ns>/<name>`)".format(prov.address))
        target_rel = "{prefix}/{rel}/{host}/{ns}/{name}/{version}/{plat}/{filename}".format(
            prefix = work_prefix,
            rel = PLUGIN_DIR_RELPATH,
            host = parts[0],
            ns = parts[1],
            name = parts[2],
            version = prov.version,
            plat = platform_key,
            filename = binary.basename,
        )
        out = ctx.actions.declare_file(target_rel)
        ctx.actions.symlink(output = out, target_file = binary)
        outputs.append(out)

    return outputs

def work_tree_root(ctx):
    """Exec-root-relative path to this rule's work tree root."""
    return "{bin}/{pkg}/{name}.work".format(
        bin = ctx.bin_dir.path,
        pkg = ctx.label.package,
        name = ctx.label.name,
    )
