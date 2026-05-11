"""`tf_runner` rule: generates an executable shell script that materializes a
deploy's transitive .tf inputs into a scratch working tree and invokes `tofu
init` followed by `tofu <command>` (plan or apply).
"""

load(":providers.bzl", "TerraformDeployInfo")
load("//toolchain:toolchain.bzl", "TOOLCHAIN_TYPE")

def _rlocation_key(workspace_name, f):
    """Compute the runfiles `rlocation` lookup key for a File `f`.

    For files in the main workspace (`short_path` does not start with `..`),
    the key is `<workspace_name>/<short_path>`. For files in external repos,
    `short_path` already starts with `../<repo>/...` and we strip the leading
    `../`.
    """
    sp = f.short_path
    if sp.startswith("../"):
        return sp[3:]
    return workspace_name + "/" + sp

def tf_runner_impl(ctx):
    deploy = ctx.attr.deploy[TerraformDeployInfo]
    tofu = ctx.toolchains[TOOLCHAIN_TYPE].tofu

    workspace_name = ctx.workspace_name or "_main"

    # Build the file-table substitution: one line per file as
    # `<workspace_relative_path>\t<rlocation_key>`. The script reads this into
    # a bash array and symlinks each into the scratch working tree.
    rows = []
    seen = {}
    for entry in deploy.transitive_files.to_list():
        path = entry.path
        if path in seen:
            # Earlier dep wins on collision; surface a hard error so users
            # don't silently get the wrong file.
            other = seen[path]
            if other != entry.file:
                fail("File collision at workspace path `{}` between `{}` and `{}`. ".format(
                    path,
                    other.path,
                    entry.file.path,
                ) + "Two libraries are contributing different content at the same workspace-relative path.")
            continue
        seen[path] = entry.file
        rows.append(path + "\t" + _rlocation_key(workspace_name, entry.file))

    file_table = "\n".join(rows)

    # Stable, short identifier used to namespace state + lock directories per
    # target. Uses the canonical label string.
    label_id = "{}_{}".format(
        ctx.label.package.replace("/", "_") or "ROOT",
        ctx.label.name.replace(".", "_"),
    )

    out = ctx.actions.declare_file(ctx.label.name + ".sh")
    ctx.actions.expand_template(
        template = ctx.file._template,
        output = out,
        is_executable = True,
        substitutions = {
            "%{command}%": ctx.attr.command,
            "%{package_dir}%": deploy.package_dir,
            "%{tofu_rlocation}%": _rlocation_key(workspace_name, tofu.binary),
            "%{label_id}%": label_id,
            "%{file_table}%": file_table,
            "%{workspace_name}%": workspace_name,
        },
    )

    files = [entry.file for entry in deploy.transitive_files.to_list()]
    files.append(tofu.binary)
    runfiles = ctx.runfiles(
        files = files,
        transitive_files = depset(transitive = [
            ctx.attr._runfiles_lib[DefaultInfo].default_runfiles.files,
        ]),
    ).merge(ctx.attr._runfiles_lib[DefaultInfo].default_runfiles)

    return [DefaultInfo(executable = out, runfiles = runfiles)]

tf_runner = rule(
    implementation = tf_runner_impl,
    executable = True,
    attrs = {
        "deploy": attr.label(
            mandatory = True,
            providers = [TerraformDeployInfo],
        ),
        "command": attr.string(
            mandatory = True,
            values = ["plan", "apply"],
        ),
        "_template": attr.label(
            default = "//terraform/private/templates:runner.sh.tpl",
            allow_single_file = True,
        ),
        "_runfiles_lib": attr.label(
            default = "@bazel_tools//tools/bash/runfiles",
        ),
    },
    toolchains = [TOOLCHAIN_TYPE],
)
