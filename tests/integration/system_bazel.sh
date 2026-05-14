#!/usr/bin/env bash
# Wrapper that delegates to the system Bazel (or Bazelisk) on PATH.
# Used by bazel_integration_test to avoid re-downloading and re-extracting the
# Bazel binary inside every test invocation — the CI runner's setup-bazel
# action has already done that.
exec bazel "$@"
