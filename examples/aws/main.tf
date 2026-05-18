terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "6.45.0"
    }
  }
}

variable "region" {
  type        = string
  description = "AWS region to deploy into."
}

variable "bucket_name" {
  type        = string
  description = "Name of the S3 bucket to manage."
}

provider "aws" {
  region = var.region

  # Real credentials are resolved at plan/apply time via the usual AWS
  # provider lookup (env vars, shared credentials file, etc.). `tofu validate`
  # — which rules_tofu runs at `bazel build` time — does not contact AWS.
}

resource "aws_s3_bucket" "example" {
  bucket = var.bucket_name
}

output "bucket_arn" {
  value = aws_s3_bucket.example.arn
}
