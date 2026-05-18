"""Public rules_tofu API.

Downstream loads:

    load("@rules_tofu//tf:defs.bzl", "tf_library", "tf_deploy")
"""

load("//tf/private:deploy.bzl", _tf_deploy = "tf_deploy")
load("//tf/private:library.bzl", _tf_library = "tf_library")

tf_library = _tf_library
tf_deploy = _tf_deploy
