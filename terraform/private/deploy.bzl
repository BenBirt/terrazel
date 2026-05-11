"""`terraform_deploy` rule and macro.

The macro emits:
  - `:<name>`       — the underlying `_terraform_deploy` data target.
  - `:<name>.plan`  — runnable: `bazel run :<name>.plan`
  - `:<name>.apply` — runnable: `bazel run :<name>.apply`
"""

load(":providers.bzl", "TerraformDeployInfo", "TerraformLibraryInfo")
load(":runner.bzl", _tf_runner = "tf_runner")

def _terraform_deploy_impl(ctx):
    # Generate terrazel.auto.tfvars.json from the `vars` dict and place it at
    # the deploy's package directory so OpenTofu auto-loads it.
    tfvars_file = ctx.actions.declare_file("terrazel.auto.tfvars.json")
    ctx.actions.write(
        output = tfvars_file,
        content = json.encode_indent({k: v for k, v in ctx.attr.vars.items()}, indent = "  "),
    )

    direct = [struct(path = f.short_path, file = f) for f in ctx.files.srcs]
    direct.append(struct(path = tfvars_file.short_path, file = tfvars_file))

    transitive = [
        d[TerraformLibraryInfo].transitive_files
        for d in ctx.attr.deps
    ]

    files = depset(direct = direct, transitive = transitive)

    return [
        DefaultInfo(files = depset(direct = ctx.files.srcs + [tfvars_file])),
        TerraformDeployInfo(
            transitive_files = files,
            package_dir = ctx.label.package,
            vars = ctx.attr.vars,
        ),
    ]

_terraform_deploy = rule(
    implementation = _terraform_deploy_impl,
    attrs = {
        "srcs": attr.label_list(
            allow_files = [".tf", ".tf.json", ".tfvars", ".tfvars.json", ".tftpl", ".hcl"],
            doc = "Optional deploy-local config files (e.g. provider/backend setup).",
        ),
        "deps": attr.label_list(
            providers = [TerraformLibraryInfo],
            doc = "`terraform_library` targets this deploy composes.",
        ),
        "vars": attr.string_dict(
            doc = "Variable values bound to this deploy. Rendered to terrazel.auto.tfvars.json.",
        ),
    },
    doc = "Underlying data-carrier for `terraform_deploy`. Use the `terraform_deploy` macro.",
)

def terraform_deploy(name, srcs = None, deps = None, vars = None, **kwargs):
    """A root Terraform/OpenTofu invocation.

    Generates three labels:
      - `:<name>`       — data target (the underlying rule).
      - `:<name>.plan`  — `bazel run` to produce a plan.
      - `:<name>.apply` — `bazel run` to apply the plan.

    Args:
      name: target name.
      srcs: optional deploy-local .tf files (e.g. provider/backend config).
      deps: `terraform_library` targets to compose.
      vars: dict of variable name -> value to render as terrazel.auto.tfvars.json.
      **kwargs: forwarded to the underlying rule (e.g. visibility, tags).
    """
    visibility = kwargs.pop("visibility", None)
    tags = kwargs.pop("tags", None)
    testonly = kwargs.pop("testonly", None)

    common_kwargs = {}
    if visibility != None:
        common_kwargs["visibility"] = visibility
    if tags != None:
        common_kwargs["tags"] = tags
    if testonly != None:
        common_kwargs["testonly"] = testonly

    _terraform_deploy(
        name = name,
        srcs = srcs or [],
        deps = deps or [],
        vars = vars or {},
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
