#!/usr/bin/env bash
# Wrapper that delegates to the system Bazel on PATH.
#
# On CI, the outer Bazel's extracted installation is copied to
# /tmp/bazel_install before tests run.  Using --install_base points
# the inner Bazel there, avoiding a slow re-extraction while keeping
# a separate output base (no server conflict with the outer Bazel).
if [[ -d /tmp/bazel_install ]]; then
  exec bazel --install_base=/tmp/bazel_install "$@"
else
  exec bazel "$@"
fi
