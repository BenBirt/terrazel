load("@terrazel//terraform:defs.bzl", "terraform_library")

# main.tf references `var.does_not_exist`, which `tofu validate` rejects
# with "Reference to undeclared input variable". The build-time
# validate action inside terraform_library propagates that failure.
terraform_library(
    name = "bad",
    srcs = ["main.tf"],
    fmt_test = False,
)
