terraform {
  required_version = ">= 1.5"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 6.0"
    }
  }

  backend "gcs" {
    bucket = "bulwarkai-tfstate"
  }
}

variable "project_id" {
  description = "GCP project ID"
  type        = string
}

variable "region" {
  description = "GCP region for all resources"
  type        = string
  default     = "europe-west2"
}

variable "allowed_domains" {
  description = "Email domains permitted to use the service"
  type        = string
}

variable "response_mode" {
  description = "Screening mode: strict, fast (alias: input_only), or audit (alias: buffer)"
  type        = string
  default     = "strict"
}

variable "fallback_model" {
  description = "Gemini model when none specified in request"
  type        = string
  default     = "gemini-2.5-flash"
}

variable "model_armor_template" {
  description = "Model Armor template name"
  type        = string
  default     = "test-template"
}

variable "api_keys" {
  description = "Comma-separated API keys for X-Api-Key auth"
  type        = string
  default     = ""
}

variable "user_agent_regex" {
  description = "Regex for User-Agent enforcement. Empty disables check."
  type        = string
  default     = ""
}

variable "dlp_enabled" {
  description = "Enable DLP inspector"
  type        = bool
  default     = false
}

variable "dlp_info_types" {
  description = "DLP info types to detect"
  type        = string
  default     = "US_SOCIAL_SECURITY_NUMBER,CREDIT_CARD_NUMBER,EMAIL_ADDRESS,PHONE_NUMBER"
}

variable "allowed_iam_members" {
  description = "IAM members allowed to invoke the Cloud Run service (e.g. ['user:alice@example.com', 'group:engineers@example.com'])"
  type        = list(string)
  default     = []
}

locals {
  service_name = "bulwarkai"
  sa_name      = "bulwarkai"
}

provider "google" {
  project = var.project_id
  region  = var.region
}

data "google_project" "project" {}
