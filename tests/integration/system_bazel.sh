#!/usr/bin/env bash
# Wrapper that delegates to the system Bazel on PATH.
#
# Why unset TEST_TMPDIR?  Bazel's test runner sets a per-test TEST_TMPDIR,
# which Bazel uses as the output base.  A fresh output base forces Bazel to
# re-extract its ~500MB installation.  By unsetting TEST_TMPDIR the inner
# Bazel reuses the default output_user_root (and therefore the install base)
# that the outer CI Bazel has already populated.
unset TEST_TMPDIR
exec bazel "$@"
