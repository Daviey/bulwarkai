variable "access_policy_name" {
  description = "Access Context Manager policy name (org-level, must exist). Create with: gcloud access-context-manager policies create --organization ORG_ID --title bulwarkai"
  type        = string
}

variable "org_id" {
  description = "GCP organization ID (number)"
  type        = string
}

data "google_organization" "org" {
  organization = var.org_id
}

resource "google_access_context_manager_access_level" "bulwarkai_vpc" {
  parent = "accessPolicies/${var.access_policy_name}"
  name   = "accessPolicies/${var.access_policy_name}/accessLevels/bulwarkai_vpc"
  title  = "bulwarkai_vpc"

  basic {
    conditions {
      ip_subnetworks = [google_compute_subnetwork.bulwarkai.ip_cidr_range]
    }
  }
}

resource "google_access_context_manager_service_perimeter" "bulwarkai" {
  parent         = "accessPolicies/${var.access_policy_name}"
  name           = "accessPolicies/${var.access_policy_name}/servicePerimeters/bulwarkai"
  title          = "bulwarkai"
  perimeter_type = "PERIMETER_TYPE_REGULAR"

  spec {
    restricted_services = [
      "aiplatform.googleapis.com",
      "modelarmor.googleapis.com",
      "dlp.googleapis.com",
    ]

    resources = [
      "projects/${data.google_project.project.number}",
    ]

    access_levels = [
      google_access_context_manager_access_level.bulwarkai_vpc.name,
    ]

    ingress_policies {
      ingress_from {
        identity_type = "ANY_IDENTITY"
        sources {
          access_level = google_access_context_manager_access_level.bulwarkai_vpc.name
        }
      }
      ingress_to {
        resources = ["*"]
        operations {
          service_name = "aiplatform.googleapis.com"
        }
        operations {
          service_name = "modelarmor.googleapis.com"
        }
        operations {
          service_name = "dlp.googleapis.com"
        }
      }
    }
  }

  lifecycle {
    ignore_changes = [spec[0].resources]
  }
}
