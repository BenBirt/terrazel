variable "name" {
  type        = string
  description = "Subject of the greeting."
}

locals {
  greeting = "Hello, ${var.name}!"
}

output "greeting" {
  value = local.greeting
}
