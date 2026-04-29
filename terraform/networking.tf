resource "google_compute_network" "bulwarkai" {
  name                    = "bulwarkai-vpc"
  auto_create_subnetworks = false
}

resource "google_compute_subnetwork" "bulwarkai" {
  name          = "bulwarkai-subnet"
  ip_cidr_range = "10.8.0.0/24"
  region        = var.region
  network       = google_compute_network.bulwarkai.id
}

resource "google_compute_global_address" "private_service_access" {
  name          = "bulwarkai-psa-range"
  purpose       = "VPC_PEERING"
  prefix_length = 16
  network       = google_compute_network.bulwarkai.id
  address_type  = "INTERNAL"
}

resource "google_service_networking_connection" "private_service_access" {
  network                 = google_compute_network.bulwarkai.id
  service                 = "servicenetworking.googleapis.com"
  reserved_peering_ranges = [google_compute_global_address.private_service_access.name]
}

resource "google_project_iam_member" "cloud_run_vpc_user" {
  project = var.project_id
  role    = "roles/compute.networkUser"
  member  = "serviceAccount:service-${data.google_project.project.number}@serverless-robot-prod.iam.gserviceaccount.com"
}
