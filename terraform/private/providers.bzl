"""Providers exchanged between terrazel rules."""

# Each entry in `transitive_files` is a struct(path = workspace_relative_path,
# file = File). The path is the location at which the file should appear in the
# materialized runtime working tree.
TerraformLibraryInfo = provider(
    doc = "Carries the .tf files contributed by a terraform_library and all its transitive deps.",
    fields = {
        "transitive_files": "depset[struct(path, file)] of all .tf files across the dep graph.",
    },
)

TerraformDeployInfo = provider(
    doc = "Carries everything needed to run `tofu` against a root terraform_deploy.",
    fields = {
        "transitive_files": "depset[struct(path, file)] of all .tf inputs (deps + local srcs + generated tfvars).",
        "package_dir": "string: workspace-relative directory the runner cd's into before running tofu.",
        "vars": "dict[string, string]: variable values bound at this deployment site.",
    },
)
