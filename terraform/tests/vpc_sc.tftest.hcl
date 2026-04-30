mock_provider "google" {}

run "no_vpc_sc_resources_when_disabled" {
  command = plan

  variables {
    project_id      = "test-project"
    allowed_domains = "example.com"
    vpc_sc_enabled  = false
  }

  assert {
    condition     = length(data.google_organization.org) == 0
    error_message = "org data source should not exist when vpc_sc_enabled is false"
  }

  assert {
    condition     = length(google_access_context_manager_access_level.bulwarkai_vpc) == 0
    error_message = "access level should not exist when vpc_sc_enabled is false"
  }

  assert {
    condition     = length(google_access_context_manager_service_perimeter.bulwarkai) == 0
    error_message = "perimeter should not exist when vpc_sc_enabled is false"
  }
}

run "vpc_sc_resources_created_when_enabled" {
  command = plan

  variables {
    project_id          = "test-project"
    allowed_domains     = "example.com"
    vpc_sc_enabled      = true
    access_policy_name  = "123456"
    org_id              = "789"
  }

  assert {
    condition     = length(google_access_context_manager_access_level.bulwarkai_vpc) == 1
    error_message = "access level should exist when vpc_sc_enabled is true"
  }

  assert {
    condition     = length(google_access_context_manager_service_perimeter.bulwarkai) == 1
    error_message = "perimeter should exist when vpc_sc_enabled is true"
  }
}

run "access_level_uses_vpc_subnet" {
  command = plan

  variables {
    project_id          = "test-project"
    allowed_domains     = "example.com"
    vpc_sc_enabled      = true
    access_policy_name  = "123456"
    org_id              = "789"
  }

  assert {
    condition     = google_access_context_manager_access_level.bulwarkai_vpc[0].basic[0].conditions[0].ip_subnetworks[0] == "10.8.0.0/24"
    error_message = "access level should use the Cloud Run VPC subnet CIDR"
  }
}

run "perimeter_restricts_vertex_modelarmor_dlp" {
  command = plan

  variables {
    project_id          = "test-project"
    allowed_domains     = "example.com"
    vpc_sc_enabled      = true
    access_policy_name  = "123456"
    org_id              = "789"
  }

  assert {
    condition     = contains(google_access_context_manager_service_perimeter.bulwarkai[0].spec[0].restricted_services, "aiplatform.googleapis.com")
    error_message = "perimeter should restrict Vertex AI"
  }

  assert {
    condition     = contains(google_access_context_manager_service_perimeter.bulwarkai[0].spec[0].restricted_services, "modelarmor.googleapis.com")
    error_message = "perimeter should restrict Model Armor"
  }

  assert {
    condition     = contains(google_access_context_manager_service_perimeter.bulwarkai[0].spec[0].restricted_services, "dlp.googleapis.com")
    error_message = "perimeter should restrict DLP"
  }
}

run "ingress_allows_any_identity" {
  command = plan

  variables {
    project_id          = "test-project"
    allowed_domains     = "example.com"
    vpc_sc_enabled      = true
    access_policy_name  = "123456"
    org_id              = "789"
  }

  assert {
    condition     = google_access_context_manager_service_perimeter.bulwarkai[0].spec[0].ingress_policies[0].ingress_from[0].identity_type == "ANY_IDENTITY"
    error_message = "ingress should allow ANY_IDENTITY to preserve user token passthrough"
  }
}
