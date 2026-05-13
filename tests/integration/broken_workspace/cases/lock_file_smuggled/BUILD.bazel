load("@terrazel//terraform:defs.bzl", "terraform_library")

# .terraform.lock.hcl is included in srcs. init_action.bzl detects it
# in the materialized work tree and refuses to validate.
terraform_library(
    name = "smuggled",
    srcs = [
        "main.tf",
        ".terraform.lock.hcl",
    ],
    fmt_test = False,
)
