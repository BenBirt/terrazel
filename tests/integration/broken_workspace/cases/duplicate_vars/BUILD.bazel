load("@terrazel//terraform:defs.bzl", "terraform_deploy", "terraform_library")

# `vars` and `extra.tfvars.json` both declare `foo`. The dupcheck action
# emitted by terraform_deploy must fail at build time with
# "... declared in both ...".
terraform_library(
    name = "lib",
    srcs = ["main.tf"],
    fmt_test = False,
)

terraform_deploy(
    name = "dup",
    deps = [":lib"],
    vars = {"foo": "from-vars"},
    var_files = ["extra.tfvars.json"],
    fmt_test = False,
)
