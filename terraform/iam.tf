resource "google_service_account" "bulwarkai" {
  account_id   = local.sa_name
  display_name = "Bulwarkai"
  description  = "Service account for Bulwarkai"
  project      = var.project_id
}

resource "google_project_iam_custom_role" "vertex_invoker" {
  role_id     = "vertexAIBulwarkaiInvoker"
  title       = "Vertex AI Bulwarkai Invoker"
  description = "Minimal permissions for Bulwarkai to call Vertex AI inference endpoints"
  permissions = [
    "aiplatform.endpoints.predict",
    "aiplatform.endpoints.streamPredict",
    "aiplatform.models.predict",
    "aiplatform.models.streamPredict",
  ]
  project = var.project_id
}

resource "google_project_iam_member" "vertex_invoker" {
  project = var.project_id
  role    = google_project_iam_custom_role.vertex_invoker.name
  member  = "serviceAccount:${google_service_account.bulwarkai.email}"
}

resource "google_project_iam_member" "modelarmor_user" {
  project = var.project_id
  role    = "roles/modelarmor.user"
  member  = "serviceAccount:${google_service_account.bulwarkai.email}"
}

resource "google_project_iam_member" "dlp_user" {
  count   = var.dlp_enabled ? 1 : 0
  project = var.project_id
  role    = "roles/dlp.reader"
  member  = "serviceAccount:${google_service_account.bulwarkai.email}"
}

resource "google_cloud_run_service_iam_member" "invoker" {
  count    = length(var.allowed_iam_members)
  location = google_cloud_run_v2_service.bulwarkai.location
  service  = google_cloud_run_v2_service.bulwarkai.name
  role     = "roles/run.invoker"
  member   = var.allowed_iam_members[count.index]
}

resource "google_cloud_run_service_iam_member" "self_invoke" {
  location = google_cloud_run_v2_service.bulwarkai.location
  service  = google_cloud_run_v2_service.bulwarkai.name
  role     = "roles/run.invoker"
  member   = "serviceAccount:${google_service_account.bulwarkai.email}"
}

resource "google_project_iam_audit_config" "aiplatform" {
  project = var.project_id
  service = "aiplatform.googleapis.com"
  audit_log_config {
    log_type = "DATA_READ"
  }
  audit_log_config {
    log_type = "DATA_WRITE"
  }
  audit_log_config {
    log_type = "ADMIN_READ"
  }
}
