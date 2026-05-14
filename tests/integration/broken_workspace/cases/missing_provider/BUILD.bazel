load("@terrazel//terraform:defs.bzl", "terraform_library")

# main.tf declares `required_providers { aws = ... }` but the library has
# no `providers = [...]`. The plugin tree materialized for validate is
# empty, so `tofu init -plugin-dir=<empty>` fails with
# "Failed to query available provider packages" (or similar) while
# looking up hashicorp/aws.
terraform_library(
    name = "missing",
    srcs = ["main.tf"],
    fmt_test = False,
)
