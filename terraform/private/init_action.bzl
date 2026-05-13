"""Build-time `tofu init -backend=false` + `tofu validate` for a materialized
work tree.

The deploy rule (and the library's auto-emitted validating deploy) call
`tf_init_validate(...)` to attach a validate stamp to the rule's outputs, so
`bazel build :foo` exercises validation in the build graph (with caching).

Configurations without a `required_providers` block validate offline today.
Once chunk 3 lands `terraform_library`/`terraform_deploy`'s `providers`
attribute, this helper will also accept a plugin tree and pass
`-plugin-dir=<tree>` to make the offline guarantee unconditional.
"""

load("//toolchain:toolchain.bzl", "TOOLCHAIN_TYPE")

def tf_init_validate(ctx, work_tree_files, work_tree_root, package_dir):
    """Emit a build-time `tofu init -backend=false` + `tofu validate` action.

    Args:
      ctx: rule ctx. Must list `TOOLCHAIN_TYPE` in `toolchains`.
      work_tree_files: depset[File] of every materialized work tree file.
      work_tree_root: string, exec-root-relative path to the work tree root,
          e.g. `<bin>/<pkg>/<name>.work`.
      package_dir: string, workspace-relative directory inside the work tree
          that tofu should cd into before running init/validate.

    Returns:
      File: a stamp file declared as an action output.
    """
    tofu = ctx.toolchains[TOOLCHAIN_TYPE].tofu
    stamp = ctx.actions.declare_file(ctx.label.name + ".validate.stamp")

    # tofu init writes `.terraform/` and `.terraform.lock.hcl` next to the
    # config. The materialized work tree directory in the sandbox contains
    # symlinks (Bazel-managed); to avoid any chance of conflicting with
    # sandbox read-only enforcement on input trees, copy the work tree into
    # a scratch dir (dereferencing symlinks) and run there. Throwaway.
    ctx.actions.run_shell(
        inputs = depset(direct = [tofu.binary], transitive = [work_tree_files]),
        outputs = [stamp],
        command = """\
set -euo pipefail
TOFU=$1
WORK_TREE_ROOT=$2
PACKAGE_DIR=$3
STAMP=$4
SCRATCH=$(mktemp -d "${TMPDIR:-/tmp}/terrazel-validate-XXXXXX")
trap 'rm -rf "$SCRATCH"' EXIT
cp -RL "$WORK_TREE_ROOT"/. "$SCRATCH"/
CWD="$SCRATCH/$PACKAGE_DIR"
if [ -e "$CWD/.terraform.lock.hcl" ]; then
    echo "terrazel: refusing to validate: .terraform.lock.hcl present in work tree under $PACKAGE_DIR. " \\
         "Lock files are managed implicitly via Bazel's provider pinning; remove it from srcs/data." 1>&2
    exit 1
fi
"$TOFU" -chdir="$CWD" init -backend=false -input=false
"$TOFU" -chdir="$CWD" validate
touch "$STAMP"
""",
        arguments = [
            tofu.binary.path,
            work_tree_root,
            package_dir,
            stamp.path,
        ],
        env = {"TF_IN_AUTOMATION": "1"},
        mnemonic = "TofuValidate",
        progress_message = "TofuValidate %{label}",
    )
    return stamp
