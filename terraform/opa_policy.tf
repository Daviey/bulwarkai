resource "google_storage_bucket_object" "opa_policy" {
  count = var.opa_enabled && var.opa_policy_content != "" ? 1 : 0

  name   = "opa/policy.rego"
  bucket = "${local.service_name}-config"
  content = var.opa_policy_content

  depends_on = [google_storage_bucket.config]
}

resource "google_storage_bucket" "config" {
  name     = "${local.service_name}-config"
  location = var.region

  uniform_bucket_level_access = true

  encryption {
    default_kms_key_name = google_kms_crypto_key.artifact_registry.id
  }

  depends_on = [google_project_service.secretmanager]
}

resource "google_storage_bucket_iam_member" "config_reader" {
  bucket = google_storage_bucket.config.name
  role   = "roles/storage.objectViewer"
  member = "serviceAccount:${google_service_account.bulwarkai.email}"
}
