"""Build-time `tofu init -backend=false` + `tofu validate` for a materialized
work tree.

The deploy rule (and the library's auto-emitted validating deploy) call
`tf_init_validate(...)` to attach a validate stamp to the rule's outputs, so
`bazel build :foo` exercises validation in the build graph (with caching).

`-plugin-dir` is always passed (using the vendored plugin tree symlinked into
the work tree), so init is offline regardless of whether `required_providers`
is declared. With zero providers in scope, the plugin dir is empty and init
is still a no-op for provider installation.
"""

load("//toolchain:toolchain.bzl", "TOOLCHAIN_TYPE")

def tf_init_validate(ctx, work_tree_files, work_tree_root, package_dir, plugin_dir_relpath):
    """Emit a build-time `tofu init -backend=false` + `tofu validate` action.

    Args:
      ctx: rule ctx. Must list `TOOLCHAIN_TYPE` in `toolchains`.
      work_tree_files: depset[File] of every materialized work tree file
          (including plugin binaries symlinked under `<work>/<plugin_dir_relpath>`).
      work_tree_root: string, exec-root-relative path to the work tree root,
          e.g. `<bin>/<pkg>/<name>.work`.
      package_dir: string, workspace-relative directory inside the work tree
          that tofu should cd into before running init/validate.
      plugin_dir_relpath: string, path under the work tree root where the
          vendored provider tree lives (e.g. `.rules_tofu-plugins`). Pinned via
          `-plugin-dir` so init runs offline.

    Returns:
      File: a stamp file declared as an action output.
    """
    tofu = ctx.toolchains[TOOLCHAIN_TYPE].tofu
    stamp = ctx.actions.declare_file(ctx.label.name + ".validate.stamp")

    # tofu init writes `.terraform/` and `.terraform.lock.hcl` next to the
    # config. The materialized work tree directory in the sandbox contains
    # symlinks (Bazel-managed); to avoid any chance of conflicting with
    # sandbox read-only enforcement on input trees, copy the work tree into
    # a scratch dir under $TMPDIR (dereferencing symlinks) and run there.
    # Bazel cleans up the action's sandbox on exit, so the copy is throwaway
    # — no explicit `rm -rf` needed.
    ctx.actions.run_shell(
        inputs = depset(direct = [tofu.binary], transitive = [work_tree_files]),
        outputs = [stamp],
        command = """\
set -euo pipefail
TOFU=$1
WORK_TREE_ROOT=$2
PACKAGE_DIR=$3
PLUGIN_DIR_REL=$4
STAMP=$5
SCRATCH=$(mktemp -d "${TMPDIR:-/tmp}/rules_tofu-validate-XXXXXX")
cp -RL "$WORK_TREE_ROOT"/. "$SCRATCH"/
mkdir -p "$SCRATCH/$PLUGIN_DIR_REL"
CWD="$SCRATCH/$PACKAGE_DIR"
if [ -e "$CWD/.terraform.lock.hcl" ]; then
    echo "rules_tofu: refusing to validate: .terraform.lock.hcl present in work tree under $PACKAGE_DIR. " \\
         "Lock files are managed implicitly via Bazel's provider pinning; remove it from srcs/data." 1>&2
    exit 1
fi
# Redirect init stdout to suppress "Installing provider" progress spam.
# Stderr is kept so error messages (e.g. "Failed to query available provider
# packages") and the "Incomplete lock file" warning remain visible.
# There is no flag to skip lockfile generation — the lockfile is written into
# $SCRATCH and discarded with it.
"$TOFU" -chdir="$CWD" init -backend=false -input=false -plugin-dir="$SCRATCH/$PLUGIN_DIR_REL" >/dev/null
"$TOFU" -chdir="$CWD" validate
touch "$STAMP"
""",
        arguments = [
            tofu.binary.path,
            work_tree_root,
            package_dir,
            plugin_dir_relpath,
            stamp.path,
        ],
        env = {"TF_IN_AUTOMATION": "1"},
        mnemonic = "TofuValidate",
        progress_message = "TofuValidate %{label}",
    )
    return stamp
