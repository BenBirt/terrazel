#!/usr/bin/env bash
# Wrapper that delegates to the system Bazel on PATH.
#
# If BAZEL_INSTALL_BASE is set (exported in CI from the outer Bazel),
# reuse that install base so the inner Bazel skips the ~500MB extraction.
# The inner Bazel still gets its own output base (via TEST_TMPDIR) so
# there is no server conflict with the outer Bazel.
if [[ -n "${BAZEL_INSTALL_BASE:-}" ]]; then
  exec bazel --install_base="${BAZEL_INSTALL_BASE}" "$@"
else
  exec bazel "$@"
fi
