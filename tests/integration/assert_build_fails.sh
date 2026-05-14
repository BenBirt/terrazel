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

cd "${BIT_WORKSPACE_DIR}"

timeout 60 "${BIT_BAZEL_BINARY}" build "${TARGET}" >"${LOG}" 2>&1
rc=$?
if [[ ${rc} -eq 124 ]]; then
  echo "TIMEOUT: nested 'bazel build ${TARGET}' killed after 60s" | tee -a "${LOG}" >&2
fi

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

echo "OK: ${TARGET} failed with expected fragment '${EXPECTED_PATTERN}'"
