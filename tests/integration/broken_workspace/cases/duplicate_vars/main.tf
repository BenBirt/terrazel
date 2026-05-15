variable "foo" {
  type = string
}

output "echo" {
  value = var.foo
}
