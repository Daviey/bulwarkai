resource "google_artifact_registry_repository" "bulwarkai" {
  location      = var.region
  repository_id = "bulwarkai"
  format        = "DOCKER"
  description   = "Container images for Bulwarkai (production)"
  kms_key_name  = google_kms_crypto_key.artifact_registry.id

  depends_on = [google_project_service.artifactregistry]
}

resource "google_artifact_registry_repository" "bulwarkai_nonprod" {
  location      = var.region
  repository_id = "bulwarkai-nonprod"
  format        = "DOCKER"
  description   = "Container images for Bulwarkai (non-production)"
  kms_key_name  = google_kms_crypto_key.artifact_registry.id

  depends_on = [google_project_service.artifactregistry]
}
