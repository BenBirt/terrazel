"""`terraform_deploy` rule + macro.

The macro always emits:
  - `:<name>`         — the `_terraform_deploy` data target. Its outputs are
                        the materialized working tree (a symlink-mirror of
                        every transitive .tf input at its workspace-relative
                        path, plus a generated `terrazel.auto.tfvars.json`)
                        and a validate stamp produced by running
                        `tofu init -backend=false && tofu validate` at build
                        time, so `bazel build :<name>` exercises validation.
  - `:<name>.plan`    — runnable: `bazel run :<name>.plan`
  - `:<name>.apply`   — runnable: `bazel run :<name>.apply`
  - `:<name>.destroy` — runnable: `bazel run :<name>.destroy`
  - `:<name>.fmt`     — runnable: `bazel run :<name>.fmt`

When enabled (default):
  - `:<name>.fmt_check`  — test: `bazel test :<name>.fmt_check` (fmt_test=True)

Materialization happens at analysis time via `ctx.actions.symlink` (one
action per file). At runtime the runner cd's into the work tree and
invokes `tofu init && tofu <plan|apply>` against it. Nothing is
mktemp'd; nothing is symlinked from bash.
"""

load("//toolchain:toolchain.bzl", "TOOLCHAIN_TYPE")
load(":fmt.bzl", _tf_fmt = "tf_fmt", _tf_fmt_check = "tf_fmt_check_test")
load(":init_action.bzl", _tf_init_validate = "tf_init_validate")
load(":providers.bzl", "TerraformDeployInfo", "TerraformLibraryInfo", "TerraformProviderInfo")
load(":runner.bzl", _tf_runner = "tf_runner")

# Path within the deploy's work tree at which we materialize provider plugin
# binaries. Layout under this root follows Terraform's standard plugin-dir
# convention: `<host>/<namespace>/<name>/<version>/<os>_<arch>/<binary>`.
_PLUGIN_DIR_RELPATH = ".terrazel-plugins"

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

def _materialize_plugin_tree(ctx, work_prefix, providers_depset):
    """Symlink one provider binary per (address, version) into the work tree's
    plugin dir using Terraform's canonical layout. Returns the list of
    declared symlink outputs (may be empty).

    Fails if two providers share an address but differ in version, or if
    a declared provider has no binary for the exec platform.
    """
    tofu = ctx.toolchains[TOOLCHAIN_TYPE].tofu
    platform_key = tofu.platform_key

    seen_versions = {}
    outputs = []
    for prov in providers_depset.to_list():
        prior = seen_versions.get(prov.address)
        if prior != None:
            if prior != prov.version:
                fail(
                    ("terraform_deploy `{label}` has conflicting versions for provider " +
                     "`{addr}`: `{a}` vs `{b}`. Pick one in MODULE.bazel.").format(
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
                ("terraform_deploy `{label}` requires provider `{addr}@{ver}` for exec " +
                 "platform `{plat}`, but the provider was declared without a `{plat}` " +
                 "entry in its `sha256` map. Add it in MODULE.bazel.").format(
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
            rel = _PLUGIN_DIR_RELPATH,
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

    work_prefix = ctx.label.name + ".work"

    # Aggregate providers: direct + transitive via library deps.
    direct_providers = [p[TerraformProviderInfo] for p in ctx.attr.providers]
    transitive_provider_sets = [
        d[TerraformLibraryInfo].providers
        for d in ctx.attr.deps
    ]
    providers_depset = depset(direct = direct_providers, transitive = transitive_provider_sets)
    plugin_outputs = _materialize_plugin_tree(ctx, work_prefix, providers_depset)
    outputs = outputs + plugin_outputs

    # The work tree root is the parent dir of every output. We expose
    # the first output as `work_tree`; the runner derives the root from
    # it via dirname-walking up to `<name>.work/`.
    work_tree_files = depset(direct = outputs)

    work_tree_root = "{bin}/{pkg}/{name}.work".format(
        bin = ctx.bin_dir.path,
        pkg = ctx.label.package,
        name = ctx.label.name,
    )
    validate_stamp = _tf_init_validate(
        ctx,
        work_tree_files = work_tree_files,
        work_tree_root = work_tree_root,
        package_dir = ctx.label.package,
        plugin_dir_relpath = _PLUGIN_DIR_RELPATH,
    )

    return [
        DefaultInfo(files = depset(direct = [validate_stamp], transitive = [work_tree_files])),
        TerraformDeployInfo(
            work_tree = outputs[0],
            work_tree_files = work_tree_files,
            package_dir = ctx.label.package,
            var_file_relpaths = [f.short_path for f in ctx.files.var_files],
            plugin_dir_relpath = _PLUGIN_DIR_RELPATH,
        ),
    ]

terraform_deploy_rule = rule(
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
        "providers": attr.label_list(
            providers = [TerraformProviderInfo],
            doc = "Vendored OpenTofu/Terraform providers this deploy declares directly. " +
                  "Unioned with providers contributed transitively by `deps`. " +
                  "Each entry is typically a `@<repo>//:provider` target exposed by " +
                  "`terraform_providers.provider(...)` in MODULE.bazel.",
        ),
    },
    toolchains = [TOOLCHAIN_TYPE],
    doc = "Underlying data-carrier for `terraform_deploy`. Use the macro.",
)

def terraform_deploy(name, srcs = None, deps = None, vars = None, var_files = None, data = None, providers = None, fmt_test = True, **kwargs):
    """A root Terraform/OpenTofu invocation.

    Always generates:
      - `:<name>`         — data target. Building it materializes the work tree
                            and runs `tofu init -backend=false && tofu validate`
                            against it, so `bazel build :<name>` validates.
      - `:<name>.plan`    — `bazel run` to produce a plan.
      - `:<name>.apply`   — `bazel run` to apply.
      - `:<name>.destroy` — `bazel run` to destroy all managed resources.
      - `:<name>.fmt`     — `bazel run` to reformat .tf files in-place.

    When enabled (default True):
      - `:<name>.fmt_check` — `bazel test` that fails if files are not formatted (fmt_test=True).

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
      providers: `terraform_provider` targets (typically `@<repo>//:provider` exposed
          by `terraform_providers.provider(...)` in MODULE.bazel) declared at the deploy
          level. Unioned with providers transitively contributed by `deps`. The exec
          platform binary is symlinked into the work tree's plugin dir; `tofu init`
          runs offline against it.
      fmt_test: whether to emit a `:<name>.fmt_check` test target (default True).
      **kwargs: forwarded to the underlying rule (visibility, tags, testonly).
    """
    common_kwargs = {}
    for forwarded in ("visibility", "tags", "testonly"):
        if forwarded in kwargs:
            common_kwargs[forwarded] = kwargs.pop(forwarded)

    terraform_deploy_rule(
        name = name,
        srcs = srcs or [],
        deps = deps or [],
        vars = vars or {},
        var_files = var_files or [],
        data = data or [],
        providers = providers or [],
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

    _tf_fmt(name = name + ".fmt")
    if fmt_test:
        _tf_fmt_check(
            name = name + ".fmt_check",
            srcs = srcs or [],
        )
