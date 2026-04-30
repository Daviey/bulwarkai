mock_provider "google" {}

variables {
  project_id        = "test-project"
  allowed_domains   = "example.com"
  api_keys          = "key-one,key-two"
}

run "secret_version_created" {
  command = plan

  assert {
    condition     = length(google_secret_manager_secret_version.api_keys) == 1
    error_message = "Secret version should exist when api_keys is provided"
  }

  assert {
    condition     = google_secret_manager_secret_version.api_keys[0].secret_data == "key-one,key-two"
    error_message = "Secret data should match api_keys variable"
  }
}

run "secret_region_pinned" {
  command = plan

  assert {
    condition     = length(google_secret_manager_secret.api_keys.replication[0].user_managed[0].replicas) == 1
    error_message = "Secret should use region-pinned replication"
  }
}
