"""Public terrazel API.

Downstream loads:

    load("@terrazel//terraform:defs.bzl", "terraform_library", "terraform_deploy", "terraform_fmt")
"""

load("//terraform/private:deploy.bzl", _terraform_deploy = "terraform_deploy")
load("//terraform/private:fmt.bzl", _terraform_fmt = "terraform_fmt")
load("//terraform/private:library.bzl", _terraform_library = "terraform_library")

terraform_library = _terraform_library
terraform_deploy = _terraform_deploy
terraform_fmt = _terraform_fmt
