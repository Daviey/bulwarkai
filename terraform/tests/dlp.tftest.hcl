mock_provider "google" {}

variables {
  project_id      = "test-project"
  allowed_domains = "example.com"
  dlp_enabled     = true
  dlp_info_types  = "US_SOCIAL_SECURITY_NUMBER,CREDIT_CARD_NUMBER"
}

run "dlp_iam_created" {
  command = plan

  assert {
    condition     = length(google_project_iam_member.dlp_user) == 1
    error_message = "DLP IAM member should exist when dlp_enabled is true"
  }

  assert {
    condition     = google_project_iam_member.dlp_user[0].role == "roles/dlp.reader"
    error_message = "DLP role should be dlp.reader"
  }
}

run "dlp_env_vars_set" {
  command = plan

  assert {
    condition     = anytrue([for e in google_cloud_run_v2_service.bulwarkai.template[0].containers[0].env : e.name == "DLP_API" && e.value == "true"])
    error_message = "DLP_API env should be 'true' when dlp_enabled"
  }

  assert {
    condition     = anytrue([for e in google_cloud_run_v2_service.bulwarkai.template[0].containers[0].env : e.name == "DLP_INFO_TYPES" && e.value == "US_SOCIAL_SECURITY_NUMBER,CREDIT_CARD_NUMBER"])
    error_message = "DLP_INFO_TYPES should be set from dlp_info_types variable"
  }
}
