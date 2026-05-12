"""`terraform_deploy` rule + macro.

The macro emits four labels:
  - `:<name>`         — the `_terraform_deploy` data target. Its outputs are
                        the materialized working tree (a symlink-mirror of
                        every transitive .tf input at its workspace-relative
                        path, plus a generated `terrazel.auto.tfvars.json`).
  - `:<name>.plan`    — runnable: `bazel run :<name>.plan`
  - `:<name>.apply`   — runnable: `bazel run :<name>.apply`
  - `:<name>.destroy` — runnable: `bazel run :<name>.destroy`

Materialization happens at analysis time via `ctx.actions.symlink` (one
action per file). At runtime the runner cd's into the work tree and
invokes `tofu init && tofu <plan|apply>` against it. Nothing is
mktemp'd; nothing is symlinked from bash.
"""

load(":providers.bzl", "TerraformDeployInfo", "TerraformLibraryInfo")
load(":runner.bzl", _tf_runner = "tf_runner")

_ALLOWED_EXTS = [".tf", ".tf.json", ".tftpl", ".hcl"]

def _materialize(ctx, entries, tfvars_content):
    """Materialize the work tree under `<pkg>/<name>.work/`.

    For each `struct(path, file)`, declares `<name>.work/<path>` and
    symlinks it to `file`. Then writes
    `<name>.work/<package>/terrazel.auto.tfvars.json` from
    `tfvars_content`.

    Returns the list of declared output Files.
    """
    work_prefix = ctx.label.name + ".work"
    outputs = []
    seen = {}
    for entry in entries:
        path = entry.path
        if path.startswith("../"):
            fail(
                "terraform_deploy `{}` would include `{}` from an external ".format(
                    ctx.label,
                    entry.file.path,
                ) + "Bazel module. Terraform has no addressing scheme for files outside " +
                "the workspace root, so this is not supported. Bring the file " +
                "in-workspace (e.g. via a `genrule` or a local copy) and depend on that instead.",
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

    tfvars_path = work_prefix + "/" + ctx.label.package + "/terrazel.auto.tfvars.json"
    if tfvars_path[len(work_prefix) + 1:] in seen:
        fail(
            "`{}` collides with the generated terrazel.auto.tfvars.json. ".format(
                seen[tfvars_path[len(work_prefix) + 1:]].path,
            ) + "Rename or remove that file; deploy `vars` is the sole producer of tfvars.",
        )
    tfvars_file = ctx.actions.declare_file(tfvars_path)
    ctx.actions.write(output = tfvars_file, content = tfvars_content)
    outputs.append(tfvars_file)

    return outputs

def _terraform_deploy_impl(ctx):
    direct = [struct(path = f.short_path, file = f) for f in ctx.files.srcs + ctx.files.data]
    var_file_entries = [struct(path = f.short_path, file = f) for f in ctx.files.var_files]
    transitive = [d[TerraformLibraryInfo].transitive_files for d in ctx.attr.deps]
    entries = depset(direct = direct + var_file_entries, transitive = transitive).to_list()

    tfvars_content = json.encode_indent(
        {k: v for k, v in ctx.attr.vars.items()},
        indent = "  ",
    )

    outputs = _materialize(ctx, entries, tfvars_content)

    # The work tree root is the parent dir of every output. We expose
    # the first output as `work_tree`; the runner derives the root from
    # it via dirname-walking up to `<name>.work/`.
    work_tree_files = depset(direct = outputs)

    return [
        DefaultInfo(files = work_tree_files),
        TerraformDeployInfo(
            work_tree = outputs[0],
            work_tree_files = work_tree_files,
            package_dir = ctx.label.package,
            var_file_relpaths = [f.short_path for f in ctx.files.var_files],
        ),
    ]

_terraform_deploy = rule(
    implementation = _terraform_deploy_impl,
    attrs = {
        "srcs": attr.label_list(
            allow_files = _ALLOWED_EXTS,
            doc = "Optional deploy-local config files (e.g. provider/backend setup).",
        ),
        "deps": attr.label_list(
            providers = [TerraformLibraryInfo],
            doc = "`terraform_library` targets this deploy composes.",
        ),
        "vars": attr.string_dict(
            doc = "Variable values bound to this deploy. Rendered to terrazel.auto.tfvars.json.",
        ),
        "var_files": attr.label_list(
            allow_files = [".tfvars.json"],
            doc = "Variable-value files passed to every tofu invocation via -var-file. " +
                  "Accepts any Label producing a .tfvars.json file (e.g. a genrule output). " +
                  "Keys in var_files must not overlap with keys in vars or other var_files entries.",
        ),
        "data": attr.label_list(
            allow_files = True,
            doc = "Arbitrary files to include in the work tree. " +
                  "Use to expose files for `file()` calls in Terraform configs.",
        ),
    },
    doc = "Underlying data-carrier for `terraform_deploy`. Use the macro.",
)

def terraform_deploy(name, srcs = None, deps = None, vars = None, var_files = None, data = None, **kwargs):
    """A root Terraform/OpenTofu invocation.

    Generates four labels:
      - `:<name>`         — data target (the materialized work tree).
      - `:<name>.plan`    — `bazel run` to produce a plan.
      - `:<name>.apply`   — `bazel run` to apply.
      - `:<name>.destroy` — `bazel run` to destroy all managed resources.

    Args:
      name: target name.
      srcs: optional deploy-local .tf files (e.g. provider/backend config).
      deps: `terraform_library` targets to compose.
      vars: dict of variable name -> value, rendered to terrazel.auto.tfvars.json.
      var_files: Labels producing .tfvars.json files passed to tofu via -var-file.
          Accepts any Bazel Label (e.g. a genrule output). Keys must not overlap
          with vars or other var_files entries; duplicate keys are caught at runtime.
      data: arbitrary files to include in the work tree, enabling `file()` calls
          in Terraform configs.
      **kwargs: forwarded to the underlying rule (visibility, tags, testonly).
    """
    common_kwargs = {}
    for forwarded in ("visibility", "tags", "testonly"):
        if forwarded in kwargs:
            common_kwargs[forwarded] = kwargs.pop(forwarded)

    _terraform_deploy(
        name = name,
        srcs = srcs or [],
        deps = deps or [],
        vars = vars or {},
        var_files = var_files or [],
        data = data or [],
        **dict(common_kwargs, **kwargs)
    )

    _tf_runner(
        name = name + ".plan",
        deploy = ":" + name,
        command = "plan",
        **common_kwargs
    )

    _tf_runner(
        name = name + ".apply",
        deploy = ":" + name,
        command = "apply",
        **common_kwargs
    )

    _tf_runner(
        name = name + ".destroy",
        deploy = ":" + name,
        command = "destroy",
        **common_kwargs
    )
