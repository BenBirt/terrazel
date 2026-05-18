terraform {
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "5.45.0"
    }
  }
}

variable "project" {
  type        = string
  description = "GCP project ID to deploy into."
}

variable "region" {
  type        = string
  description = "Default region for regional resources."
}

variable "bucket_name" {
  type        = string
  description = "Name of the Cloud Storage bucket to manage."
}

provider "google" {
  project = var.project
  region  = var.region

  # Credentials are resolved at plan/apply time via the usual GCP provider
  # lookup (GOOGLE_APPLICATION_CREDENTIALS, gcloud ADC, etc.). `tofu validate`
  # — which rules_tofu runs at `bazel build` time — does not contact GCP.
}

resource "google_storage_bucket" "example" {
  name          = var.bucket_name
  location      = "US"
  force_destroy = true

  uniform_bucket_level_access = true
}

output "bucket_url" {
  value = google_storage_bucket.example.url
}
