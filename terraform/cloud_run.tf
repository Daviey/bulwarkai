resource "google_cloud_run_v2_service" "bulwarkai" {
  name     = local.service_name
  location = var.region
  ingress  = "INGRESS_TRAFFIC_INTERNAL_LOAD_BALANCER"

  template {
    service_account = google_service_account.bulwarkai.email
    encryption_key  = google_kms_crypto_key.cloud_run.id

    vpc_access {
      network_interfaces {
        network    = google_compute_network.bulwarkai.id
        subnetwork = google_compute_subnetwork.bulwarkai.id
      }
      egress = "ALL_TRAFFIC"
    }

    containers {
      image = "${google_artifact_registry_repository.bulwarkai.location}-docker.pkg.dev/${var.project_id}/${google_artifact_registry_repository.bulwarkai.repository_id}/${local.service_name}:latest"

      ports {
        container_port = 8080
      }

      env {
        name  = "GOOGLE_CLOUD_PROJECT"
        value = var.project_id
      }
      env {
        name  = "GOOGLE_CLOUD_LOCATION"
        value = var.region
      }
      env {
        name  = "ALLOWED_DOMAINS"
        value = var.allowed_domains
      }
      env {
        name  = "FALLBACK_GEMINI_MODEL"
        value = var.fallback_model
      }
      env {
        name  = "RESPONSE_MODE"
        value = var.response_mode
      }
      env {
        name  = "MODEL_ARMOR_TEMPLATE"
        value = var.model_armor_template
      }
      env {
        name  = "MODEL_ARMOR_LOCATION"
        value = var.region
      }
      env {
        name = "API_KEYS"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.api_keys.secret_id
            version = "latest"
          }
        }
      }
      dynamic "env" {
        for_each = var.user_agent_regex != "" ? [var.user_agent_regex] : []
        content {
          name  = "USER_AGENT_REGEX"
          value = env.value
        }
      }
      dynamic "env" {
        for_each = var.dlp_enabled ? ["true"] : []
        content {
          name  = "DLP_API"
          value = env.value
        }
      }
      dynamic "env" {
        for_each = var.dlp_enabled ? [var.dlp_info_types] : []
        content {
          name  = "DLP_INFO_TYPES"
          value = env.value
        }
      }
      dynamic "env" {
        for_each = var.opa_enabled ? ["true"] : []
        content {
          name  = "OPA_ENABLED"
          value = env.value
        }
      }
      dynamic "env" {
        for_each = var.opa_enabled && var.opa_policy_content != "" ? ["https://storage.googleapis.com/${local.service_name}-config/opa/policy.rego"] : []
        content {
          name  = "OPA_POLICY_URL"
          value = env.value
        }
      }
      dynamic "env" {
        for_each = var.rate_limit > 0 ? [tostring(var.rate_limit)] : []
        content {
          name  = "RATE_LIMIT"
          value = env.value
        }
      }
      dynamic "env" {
        for_each = var.rate_limit > 0 ? [var.rate_limit_window] : []
        content {
          name  = "RATE_LIMIT_WINDOW"
          value = env.value
        }
      }
      dynamic "env" {
        for_each = var.webhook_url != "" ? [var.webhook_url] : []
        content {
          name  = "WEBHOOK_URL"
          value = env.value
        }
      }
      dynamic "env" {
        for_each = var.webhook_secret != "" ? [var.webhook_secret] : []
        content {
          name  = "WEBHOOK_SECRET"
          value = env.value
        }
      }
      dynamic "env" {
        for_each = var.cors_origin != "" ? [var.cors_origin] : []
        content {
          name  = "CORS_ORIGIN"
          value = env.value
        }
      }

      resources {
        limits = {
          cpu    = "2"
          memory = "1Gi"
        }
      }

      startup_probe {
        initial_delay_seconds = 5
        timeout_seconds       = 1
        period_seconds        = 5
        failure_threshold     = 3
        http_get {
          path = "/health"
          port = 8080
        }
      }

      liveness_probe {
        initial_delay_seconds = 10
        timeout_seconds       = 1
        period_seconds        = 10
        failure_threshold     = 3
        http_get {
          path = "/health"
          port = 8080
        }
      }
    }

    timeout = "300s"
  }

  traffic {
    type    = "TRAFFIC_TARGET_ALLOCATION_TYPE_LATEST"
    percent = 100
  }

  depends_on = [
    google_project_service.run,
    google_artifact_registry_repository.bulwarkai,
    google_model_armor_template.floor,
    google_secret_manager_secret.api_keys,
    google_compute_subnetwork.bulwarkai,
  ]
}

output "service_url" {
  value = google_cloud_run_v2_service.bulwarkai.uri
}

output "service_name" {
  value = google_cloud_run_v2_service.bulwarkai.name
}
