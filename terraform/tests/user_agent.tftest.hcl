mock_provider "google" {}

variables {
  project_id        = "test-project"
  allowed_domains   = "example.com"
  user_agent_regex  = "^opencode/"
}

run "user_agent_env_set" {
  command = plan

  assert {
    condition     = anytrue([for e in google_cloud_run_v2_service.bulwarkai.template[0].containers[0].env : e.name == "USER_AGENT_REGEX" && e.value == "^opencode/"])
    error_message = "USER_AGENT_REGEX env should be set when variable is provided"
  }
}

run "no_user_agent_env_when_empty" {
  command = plan

  variables {
    user_agent_regex = ""
  }

  assert {
    condition     = !anytrue([for e in google_cloud_run_v2_service.bulwarkai.template[0].containers[0].env : e.name == "USER_AGENT_REGEX"])
    error_message = "USER_AGENT_REGEX env should not exist when variable is empty"
  }
}
