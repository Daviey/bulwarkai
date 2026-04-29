resource "google_kms_key_ring" "bulwarkai" {
  name     = "bulwarkai"
  location = var.region
}

resource "google_kms_crypto_key" "artifact_registry" {
  name            = "artifact-registry"
  key_ring        = google_kms_key_ring.bulwarkai.id
  rotation_period = "7776000s"
}

resource "google_kms_crypto_key" "cloud_run" {
  name            = "cloud-run"
  key_ring        = google_kms_key_ring.bulwarkai.id
  rotation_period = "7776000s"
}

resource "google_kms_crypto_key_iam_member" "artifact_registry_encrypt" {
  crypto_key_id = google_kms_crypto_key.artifact_registry.id
  role          = "roles/cloudkms.cryptoKeyEncrypterDecrypter"
  member        = "serviceAccount:service-${data.google_project.project.number}@gcp-sa-artifactregistry.iam.gserviceaccount.com"
}

resource "google_kms_crypto_key_iam_member" "cloud_run_encrypt" {
  crypto_key_id = google_kms_crypto_key.cloud_run.id
  role          = "roles/cloudkms.cryptoKeyEncrypterDecrypter"
  member        = "serviceAccount:service-${data.google_project.project.number}@serverless-robot-prod.iam.gserviceaccount.com"
}
