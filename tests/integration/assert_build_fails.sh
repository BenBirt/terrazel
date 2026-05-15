#!/usr/bin/env bash
# Driver for //tests/integration:*_test targets. The bazel_integration_test
# rule sets:
#   BIT_BAZEL_BINARY   — path to the nested Bazel binary
#   TARGET             — target to build inside the sub-workspace
#   EXPECTED_PATTERN   — substring expected in the failing build's stderr
# We invoke the nested Bazel and assert: non-zero exit AND stderr contains
# the expected fragment. Anything else is a test failure.

set -uo pipefail

# Save TEST_TMPDIR for our log file, then unset it so the nested Bazel
# uses the default output user root.  With TEST_TMPDIR set (the outer Bazel
# sets this per-test), the nested Bazel would extract a fresh install base
# into a throwaway temp dir — redundantly re-downloading and re-extracting
# Bazel on every single test invocation.
_TMPDIR="${TEST_TMPDIR:-/tmp}"
LOG="${_TMPDIR}/build.log"
unset TEST_TMPDIR

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
echo "  TARGET:           ${TARGET}" >&2
echo "  EXPECTED_PATTERN: ${EXPECTED_PATTERN}" >&2
echo "  BIT_BAZEL_BINARY: ${BIT_BAZEL_BINARY}" >&2
echo "  BIT_WORKSPACE_DIR: ${BIT_WORKSPACE_DIR}" >&2
echo "  PWD:              $(pwd)" >&2
echo "  date:             $(date -u)" >&2

cd "${BIT_WORKSPACE_DIR}"

echo "  workspace dir:    $(pwd)" >&2
echo "--- starting nested bazel build at $(date -u) ---" >&2

# Use tee so nested Bazel output streams to stderr in real time (visible in
# test logs) while also being captured in $LOG for the grep assertion below.
"${BIT_BAZEL_BINARY}" build "${TARGET}" 2>&1 | tee "${LOG}" >&2
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
