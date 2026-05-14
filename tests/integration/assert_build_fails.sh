#!/usr/bin/env bash
# Driver for //tests/integration:*_test targets. The bazel_integration_test
# rule sets:
#   BIT_BAZEL_BINARY   — path to the nested Bazel binary
#   TARGET             — target to build inside the sub-workspace
#   EXPECTED_PATTERN   — substring expected in the failing build's stderr
# We invoke the nested Bazel and assert: non-zero exit AND stderr contains
# the expected fragment. Anything else is a test failure.

set -uo pipefail

LOG="${TEST_TMPDIR:-/tmp}/build.log"

# Diagnostics — these go to stderr so they appear in test output even on timeout.
echo "=== assert_build_fails ===" >&2
echo "  TARGET:           ${TARGET}" >&2
echo "  EXPECTED_PATTERN: ${EXPECTED_PATTERN}" >&2
echo "  BIT_BAZEL_BINARY: ${BIT_BAZEL_BINARY}" >&2
echo "  BIT_WORKSPACE_DIR: ${BIT_WORKSPACE_DIR}" >&2
echo "  TEST_TMPDIR:      ${TEST_TMPDIR:-<unset>}" >&2
echo "  PWD:              $(pwd)" >&2
echo "  date:             $(date -u)" >&2

cd "${BIT_WORKSPACE_DIR}"

echo "  workspace dir:    $(pwd)" >&2
echo "--- starting nested bazel build at $(date -u) ---" >&2

"${BIT_BAZEL_BINARY}" build "${TARGET}" >"${LOG}" 2>&1
rc=$?

echo "--- nested bazel build finished at $(date -u), exit code: ${rc} ---" >&2

if [[ ${rc} -eq 0 ]]; then
  echo "FAIL: build of ${TARGET} succeeded; expected failure" >&2
  cat "${LOG}" >&2
  exit 1
fi

if ! grep -q -F -- "${EXPECTED_PATTERN}" "${LOG}"; then
  echo "FAIL: ${TARGET} failed but stderr did not contain '${EXPECTED_PATTERN}'" >&2
  cat "${LOG}" >&2
  exit 1
fi

echo "OK: ${TARGET} failed with expected fragment '${EXPECTED_PATTERN}'" >&2
