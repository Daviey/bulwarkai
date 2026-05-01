mock_provider "google" {}

variables {
  project_id      = "test-project"
  allowed_domains = "example.com"
  response_mode   = "audit"
  fallback_model  = "gemini-2.5-pro"
}

run "custom_response_mode" {
  command = plan

  assert {
    condition     = anytrue([for e in google_cloud_run_v2_service.bulwarkai.template[0].containers[0].env : e.name == "RESPONSE_MODE" && e.value == "audit"])
    error_message = "RESPONSE_MODE env should reflect custom value"
  }
}

run "custom_fallback_model" {
  command = plan

  assert {
    condition     = anytrue([for e in google_cloud_run_v2_service.bulwarkai.template[0].containers[0].env : e.name == "FALLBACK_GEMINI_MODEL" && e.value == "gemini-2.5-pro"])
    error_message = "FALLBACK_GEMINI_MODEL env should reflect custom value"
  }
}
