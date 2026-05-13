#!/usr/bin/env bash
# Driver for //tests/integration:*_test targets. The bazel_integration_test
# rule sets:
#   BIT_BAZEL_BINARY   — path to the nested Bazel binary
#   TARGET             — target to build inside the sub-workspace
#   EXPECTED_PATTERN   — substring expected in the failing build's stderr
# We invoke the nested Bazel and assert: non-zero exit AND stderr contains
# the expected fragment. Anything else is a test failure.
#
# The sub-workspace ships its BUILD and MODULE files with a `.tpl`
# extension so the outer terrazel Bazel never analyzes intentionally-
# failing targets nor treats sub-MODULE.bazel as a repo boundary marker.
# We materialize them just before invoking the nested Bazel.

set -uo pipefail

LOG="${TEST_TMPDIR:-/tmp}/build.log"

cd "${BIT_WORKSPACE_DIR}"

find . -name 'BUILD.tpl' -print0 |
  while IFS= read -r -d '' tpl; do
    cp "${tpl}" "$(dirname "${tpl}")/BUILD.bazel"
  done

find . -name 'MODULE.bazel.tpl' -print0 |
  while IFS= read -r -d '' tpl; do
    cp "${tpl}" "$(dirname "${tpl}")/MODULE.bazel"
  done

"${BIT_BAZEL_BINARY}" build "${TARGET}" >"${LOG}" 2>&1
rc=$?

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
