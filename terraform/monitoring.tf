resource "google_monitoring_alert_policy" "inspector_fail_open" {
  count        = var.opa_enabled || var.dlp_enabled ? 1 : 0
  display_name = "Bulwarkai Inspector Fail-Open"
  combiner     = "OR"

  conditions {
    display_name = "Inspector returning errors (fail-open)"
    condition_threshold {
      filter          = "metric.type=\"custom.googleapis.com/bulwarkai/inspector_results_total\" resource.type=\"cloud_run_revision\" metric.label.result=\"error\""
      duration        = "300s"
      comparison      = "COMPARISON_GT"
      threshold_value = 0

      aggregations {
        alignment_period   = "300s"
        per_series_aligner = "ALIGN_RATE"
      }
    }
  }

  notification_channels = []
}

resource "google_monitoring_alert_policy" "high_deny_rate" {
  count        = var.opa_enabled ? 1 : 0
  display_name = "Bulwarkai High Policy Deny Rate"
  combiner     = "OR"

  conditions {
    display_name = "Policy deny rate above threshold"
    condition_threshold {
      filter          = "metric.type=\"custom.googleapis.com/bulwarkai/policy_results_total\" resource.type=\"cloud_run_revision\" metric.label.result=\"deny\""
      duration        = "300s"
      comparison      = "COMPARISON_GT"
      threshold_value = 0.05

      aggregations {
        alignment_period   = "300s"
        per_series_aligner = "ALIGN_RATE"
      }
    }
  }

  notification_channels = []
}

resource "google_monitoring_alert_policy" "high_latency" {
  display_name = "Bulwarkai High Request Latency"
  combiner     = "OR"

  conditions {
    display_name = "P99 latency above 10 seconds"
    condition_threshold {
      filter          = "metric.type=\"custom.googleapis.com/bulwarkai/request_duration_seconds\" resource.type=\"cloud_run_revision\""
      duration        = "300s"
      comparison      = "COMPARISON_GT"
      threshold_value = 10

      aggregations {
        alignment_period   = "300s"
        per_series_aligner = "ALIGN_PERCENTILE_99"
        group_by_fields   = []
      }
    }
  }

  notification_channels = []
}

resource "google_monitoring_dashboard" "bulwarkai" {
  dashboard_json = <<EOF
{
  "displayName": "Bulwarkai Safety Proxy",
  "mosaicLayout": {
    "columns": 3,
    "tiles": [
      {
        "width": 3, "height": 2,
        "widget": {
          "title": "Request Rate by Action",
          "xyChart": {
            "dataSets": [{
              "timeSeriesQuery": {
                "timeSeriesFilter": {
                  "filter": "metric.type=\"custom.googleapis.com/bulwarkai/requests_total\" resource.type=\"cloud_run_revision\"",
                  "aggregation": {
                    "alignmentPeriod": "300s",
                    "perSeriesAligner": "ALIGN_RATE"
                  }
                }
              }
            }]
          }
        }
      },
      {
        "width": 3, "height": 2,
        "widget": {
          "title": "Inspector Results",
          "xyChart": {
            "dataSets": [{
              "timeSeriesQuery": {
                "timeSeriesFilter": {
                  "filter": "metric.type=\"custom.googleapis.com/bulwarkai/inspector_results_total\" resource.type=\"cloud_run_revision\"",
                  "aggregation": {
                    "alignmentPeriod": "300s",
                    "perSeriesAligner": "ALIGN_RATE"
                  }
                }
              }
            }]
          }
        }
      },
      {
        "width": 3, "height": 2,
        "widget": {
          "title": "Request Body Size (P99)",
          "xyChart": {
            "dataSets": [{
              "timeSeriesQuery": {
                "timeSeriesFilter": {
                  "filter": "metric.type=\"custom.googleapis.com/bulwarkai/request_body_bytes\" resource.type=\"cloud_run_revision\"",
                  "aggregation": {
                    "alignmentPeriod": "300s",
                    "perSeriesAligner": "ALIGN_PERCENTILE_99"
                  }
                }
              }
            }]
          }
        }
      }
    ]
  }
}
EOF
}
