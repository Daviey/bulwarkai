resource "google_secret_manager_secret" "api_keys" {
  secret_id = "bulwarkai-api-keys"
  replication {
    user_managed {
      replicas {
        location = var.region
      }
    }
  }
  depends_on = [google_project_service.secretmanager]
}

resource "google_secret_manager_secret_version" "api_keys" {
  count       = var.api_keys != "" ? 1 : 0
  secret      = google_secret_manager_secret.api_keys.id
  secret_data = var.api_keys
}

resource "google_secret_manager_secret_iam_member" "api_keys_accessor" {
  secret_id = google_secret_manager_secret.api_keys.id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.bulwarkai.email}"
}
