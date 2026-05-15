#!/usr/bin/env bash
# Driver for //tests/integration:*_test targets. The bazel_integration_test
# rule sets:
#   BIT_BAZEL_BINARY   — path to the nested Bazel binary
#   TARGET             — target to build inside the sub-workspace
#   EXPECTED_PATTERN   — substring expected in the failing build's stderr
# We invoke the nested Bazel and assert: non-zero exit AND stderr contains
# the expected fragment. Anything else is a test failure.

set -uo pipefail

# Use TEST_TMPDIR for our temp files; this is the per-test scratch area
# provided by the outer Bazel.
_TMPDIR="${TEST_TMPDIR:-/tmp}"
LOG="${_TMPDIR}/build.log"

# Force the nested Bazel to use its own output base inside the test's temp
# directory.  Without this, it inherits the global --output_base from
# ~/.bazelrc (set by setup-bazel), which points to the SAME output base the
# outer Bazel is using.  The nested Bazel then tries to kill the outer
# Bazel's server to take over — deadlocking because the outer Bazel is the
# process running this test.
#
# See: https://github.com/bazel-contrib/setup-bazel/issues/108
NESTED_OUTPUT_BASE="${_TMPDIR}/nested_output_base"

# Ensure partial log is always visible, even when the test is killed by timeout.
dump_log() {
  if [ -s "${LOG}" ]; then
    echo "--- nested bazel log (${LOG}) ---" >&2
    cat "${LOG}" >&2
    echo "--- end nested bazel log ---" >&2
  else
    echo "--- nested bazel log is empty or missing ---" >&2
  fi
}
trap dump_log EXIT

# Diagnostics — these go to stderr so they appear in test output even on timeout.
echo "=== assert_build_fails ===" >&2
echo "  TARGET:             ${TARGET}" >&2
echo "  EXPECTED_PATTERN:   ${EXPECTED_PATTERN}" >&2
echo "  BIT_BAZEL_BINARY:   ${BIT_BAZEL_BINARY}" >&2
echo "  BIT_WORKSPACE_DIR:  ${BIT_WORKSPACE_DIR}" >&2
echo "  TEST_TMPDIR:        ${_TMPDIR}" >&2
echo "  NESTED_OUTPUT_BASE: ${NESTED_OUTPUT_BASE}" >&2
echo "  PWD:                $(pwd)" >&2
echo "  date:               $(date -u)" >&2

cd "${BIT_WORKSPACE_DIR}"

echo "  workspace dir:    $(pwd)" >&2
echo "--- starting nested bazel build at $(date -u) ---" >&2

# Use tee so nested Bazel output streams to stderr in real time (visible in
# test logs) while also being captured in $LOG for the grep assertion below.
#
# --output_base is a STARTUP option (before "build") so it overrides any
# --output_base set in ~/.bazelrc.
"${BIT_BAZEL_BINARY}" --output_base="${NESTED_OUTPUT_BASE}" build "${TARGET}" 2>&1 | tee "${LOG}" >&2
rc=${PIPESTATUS[0]}

echo "--- nested bazel build finished at $(date -u), exit code: ${rc} ---" >&2

if [[ ${rc} -eq 0 ]]; then
  echo "FAIL: build of ${TARGET} succeeded; expected failure" >&2
  exit 1
fi

if ! grep -q -F -- "${EXPECTED_PATTERN}" "${LOG}"; then
  echo "FAIL: ${TARGET} failed but stderr did not contain '${EXPECTED_PATTERN}'" >&2
  cat "${LOG}" >&2
  exit 1
fi

echo "OK: ${TARGET} failed with expected fragment '${EXPECTED_PATTERN}'" >&2
