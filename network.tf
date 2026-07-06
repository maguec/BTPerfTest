resource "google_compute_address" "static" {
  project    = var.gcp_project_id
  region     = join("-", slice(split("-", var.gcp_zone), 0, 2))
  name       = "ipv4-${random_id.server.hex}"
  depends_on = [google_project_service.compute]
}

resource "google_compute_network" "vpc" {
  project                 = var.gcp_project_id
  name                    = "vpc-${random_id.server.hex}"
  auto_create_subnetworks = false # Best practice for test to prevent IP overlaps
  depends_on              = [google_project_service.compute]
}

resource "google_compute_firewall" "full_access_for_user" {
  project = var.gcp_project_id
  name    = "firewall-${random_id.server.hex}"
  network = google_compute_network.vpc.name

  allow {
    protocol = "tcp"
    ports    = ["22", "8000", "6379"] # Added 6379 for Redis/Valkey access
  }

  source_ranges = ["0.0.0.0/0"]
}

#############################################################################
# Subnet Setup
#############################################################################

resource "google_compute_subnetwork" "test_subnet" {
  project       = var.gcp_project_id
  name          = "subnet-${random_id.server.hex}"
  ip_cidr_range = "10.0.0.240/28" # Ensure this range is available in your VPC
  region        = join("-", slice(split("-", var.gcp_zone), 0, 2))
  network       = google_compute_network.vpc.id
}
