mock_provider "google" {}

variables {
  project_id      = "test-project"
  allowed_domains = "example.com"
}

run "validate_defaults" {
  command = plan

  assert {
    condition     = var.region == "europe-west2"
    error_message = "region default should be europe-west2"
  }

  assert {
    condition     = var.response_mode == "strict"
    error_message = "response_mode default should be strict"
  }

  assert {
    condition     = var.fallback_model == "gemini-2.5-flash"
    error_message = "fallback_model default should be gemini-2.5-flash"
  }

  assert {
    condition     = var.model_armor_template == "test-template"
    error_message = "model_armor_template default should be test-template"
  }

  assert {
    condition     = var.dlp_enabled == false
    error_message = "dlp_enabled default should be false"
  }

  assert {
    condition     = var.api_keys == ""
    error_message = "api_keys default should be empty string"
  }

  assert {
    condition     = var.user_agent_regex == ""
    error_message = "user_agent_regex default should be empty string"
  }

  assert {
    condition     = length(var.allowed_iam_members) == 0
    error_message = "allowed_iam_members default should be empty list"
  }
}

run "default_network_config" {
  command = plan

  assert {
    condition     = google_compute_network.bulwarkai.auto_create_subnetworks == false
    error_message = "VPC should not auto-create subnets"
  }

  assert {
    condition     = google_compute_subnetwork.bulwarkai.ip_cidr_range == "10.8.0.0/24"
    error_message = "subnet CIDR should be 10.8.0.0/24"
  }
}

run "cloud_run_internal_ingress" {
  command = plan

  assert {
    condition     = google_cloud_run_v2_service.bulwarkai.ingress == "INGRESS_TRAFFIC_INTERNAL_LOAD_BALANCER"
    error_message = "Cloud Run ingress should be internal load balancer only"
  }
}

run "cloud_run_env_vars" {
  command = plan

  assert {
    condition     = anytrue([for e in google_cloud_run_v2_service.bulwarkai.template[0].containers[0].env : e.name == "GOOGLE_CLOUD_PROJECT" && e.value == "test-project"])
    error_message = "GOOGLE_CLOUD_PROJECT env should be set from project_id"
  }

  assert {
    condition     = anytrue([for e in google_cloud_run_v2_service.bulwarkai.template[0].containers[0].env : e.name == "ALLOWED_DOMAINS" && e.value == "example.com"])
    error_message = "ALLOWED_DOMAINS env should be set from allowed_domains"
  }

  assert {
    condition     = anytrue([for e in google_cloud_run_v2_service.bulwarkai.template[0].containers[0].env : e.name == "RESPONSE_MODE" && e.value == "strict"])
    error_message = "RESPONSE_MODE env should be set from response_mode"
  }
}

run "no_dlp_resources_when_disabled" {
  command = plan

  assert {
    condition     = length(google_project_iam_member.dlp_user) == 0
    error_message = "DLP IAM member should not exist when dlp_enabled is false"
  }
}

run "no_secret_version_when_no_api_keys" {
  command = plan

  assert {
    condition     = length(google_secret_manager_secret_version.api_keys) == 0
    error_message = "API keys secret version should not exist when api_keys is empty"
  }
}

run "no_invoker_bindings_when_no_members" {
  command = plan

  assert {
    condition     = length(google_cloud_run_service_iam_member.invoker) == 0
    error_message = "No invoker bindings should exist when allowed_iam_members is empty"
  }
}

run "model_armor_fail_closed" {
  command = plan

  assert {
    condition     = google_model_armor_template.floor.template_metadata[0].ignore_partial_invocation_failures == false
    error_message = "Model Armor should be configured to fail closed"
  }
}

run "binary_auth_enforced" {
  command = plan

  assert {
    condition     = google_binary_authorization_policy.bulwarkai.default_admission_rule[0].enforcement_mode == "ENFORCED_BLOCK_AND_AUDIT_LOG"
    error_message = "Binary Authorization should be enforced"
  }

  assert {
    condition     = google_binary_authorization_policy.bulwarkai.default_admission_rule[0].evaluation_mode == "REQUIRE_ATTESTATION"
    error_message = "Binary Authorization should require attestation"
  }
}
