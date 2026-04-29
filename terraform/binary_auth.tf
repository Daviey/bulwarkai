resource "google_binary_authorization_policy" "bulwarkai" {
  project = var.project_id

  admission_whitelist_patterns {
    name_pattern = "europe-west2-docker.pkg.dev/${var.project_id}/bulwarkai/*"
  }

  admission_whitelist_patterns {
    name_pattern = "gcr.io/google-appengine/*"
  }

  default_admission_rule {
    evaluation_mode  = "REQUIRE_ATTESTATION"
    enforcement_mode = "ENFORCED_BLOCK_AND_AUDIT_LOG"
    require_attestations_by = [
      google_binary_authorization_attestor.bulwarkai.name,
    ]
  }

  cluster_admission_rules {
    cluster          = "projects/${var.project_id}/locations/${var.region}/clusters/bulwarkai"
    evaluation_mode  = "REQUIRE_ATTESTATION"
    enforcement_mode = "ENFORCED_BLOCK_AND_AUDIT_LOG"
    require_attestations_by = [
      google_binary_authorization_attestor.bulwarkai.name,
    ]
  }
}

resource "google_binary_authorization_attestor" "bulwarkai" {
  name    = "bulwarkai-attestor"
  project = var.project_id

  attestation_authority_note {
    note_reference = google_container_analysis_note.bulwarkai.name
  }
}

resource "google_container_analysis_note" "bulwarkai" {
  name    = "bulwarkai-attestor-note"
  project = var.project_id

  attestation_authority {
    hint {
      human_readable_name = "Bulwarkai build attestor"
    }
  }
}
