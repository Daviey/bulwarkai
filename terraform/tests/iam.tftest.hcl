mock_provider "google" {}

variables {
  project_id          = "test-project"
  allowed_domains     = "example.com"
  allowed_iam_members = ["user:alice@example.com", "group:engineers@example.com"]
}

run "invoker_bindings_created" {
  command = plan

  assert {
    condition     = length(google_cloud_run_service_iam_member.invoker) == 2
    error_message = "Should create one invoker binding per allowed_iam_member"
  }
}

run "self_invoke_always_exists" {
  command = plan

  assert {
    condition     = google_cloud_run_service_iam_member.self_invoke.role == "roles/run.invoker"
    error_message = "Self-invoker binding should always exist"
  }
}
