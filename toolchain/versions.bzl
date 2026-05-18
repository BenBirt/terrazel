"""Pinned OpenTofu release metadata.

To update, fetch the desired release's SHA256SUMS from
https://github.com/opentofu/opentofu/releases and replace both DEFAULT_VERSION
and KNOWN_VERSIONS below.

TODO(rules_tofu): generate this file from SHA256SUMS rather than maintaining by hand.
TODO(rules_tofu): replace download with a `rules_go` build-from-source so the
toolchain is fully hermetic and cross-compilable.
"""

DEFAULT_VERSION = "1.8.5"

# Map of version -> { "<os>_<arch>": "<sha256>" }
# Keys use Bazel-style os/cpu names (see PLATFORMS below for the mapping to the
# OpenTofu release asset naming, which differs only in not having a separator).
KNOWN_VERSIONS = {
    "1.8.5": {
        "linux_amd64": "e2951ba6be8ae9427aabbd5c6f243855e8b526cb2ae6bc33a05dae22d7e82632",
        "linux_arm64": "2535e8d4979806cbf79a1b704dccf1fae45b4d50ccaee3e54c1771044db4a573",
        "darwin_amd64": "cb6d1b949691e50bed6c9cc17aefc999cf27e52200521efa9f107ce3ee08260f",
        "darwin_arm64": "c77e545ab847c0d6acd322c010f457e1a30448476945c04074a48882c4b86dfe",
        "windows_amd64": "8bcd1317392a7b1ce149c5dafc886497219f560527fe10ed0d58863120d59e67",
    },
}

# (platform_key, bazel_os_constraint, bazel_cpu_constraint, exe_suffix)
PLATFORMS = [
    ("linux_amd64", "@platforms//os:linux", "@platforms//cpu:x86_64", ""),
    ("linux_arm64", "@platforms//os:linux", "@platforms//cpu:aarch64", ""),
    ("darwin_amd64", "@platforms//os:macos", "@platforms//cpu:x86_64", ""),
    ("darwin_arm64", "@platforms//os:macos", "@platforms//cpu:aarch64", ""),
    ("windows_amd64", "@platforms//os:windows", "@platforms//cpu:x86_64", ".exe"),
]
