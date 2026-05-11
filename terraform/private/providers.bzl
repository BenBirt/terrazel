"""Providers exchanged between terrazel rules.

These providers are the typed channel by which the dep graph composes:
the `terraform_library` rule emits a `TerraformLibraryInfo` carrying its
own files plus its transitive deps' files; downstream libraries and the
root `terraform_deploy` consume it to assemble the full input set, and
the runner rule reads `TerraformDeployInfo` to learn where the
materialized working tree lives and which directory to cd into.
"""

# An entry in `transitive_files` is a `struct(path, file)`, where `path` is
# the workspace-relative path at which the file should appear in the
# materialized working tree.
TerraformLibraryInfo = provider(
    doc = "Carries the .tf files contributed by a terraform_library and all its transitive library deps.",
    fields = {
        "transitive_files": "depset[struct(path, file)] of .tf files across the dep graph.",
    },
)

# The materialized work tree is a directory output of `_terraform_deploy`
# rooted at `bazel-bin/<package>/<name>.work/`. Every transitive .tf file
# (plus the generated terrazel.auto.tfvars.json) appears under that root
# at its workspace-relative path.
TerraformDeployInfo = provider(
    doc = "Carries everything a runner needs to execute `tofu` against a root deploy.",
    fields = {
        "work_tree": "File: the materialized work tree root (a directory of symlinks + generated tfvars).",
        "work_tree_files": "depset[File]: every file inside the work tree (so runfiles include them).",
        "package_dir": "string: workspace-relative directory the runner cd's into before running tofu.",
        "state_id": "string: stable identifier used to namespace state under bazel-out/terrazel/.",
    },
)
