provider "google" {
  project = var.gcp_project_id
  region  = join("-", slice(split("-", var.gcp_zone), 0, 2))
}

resource "random_id" "server" {
  byte_length = 8
}

locals {
  #bt_count = var.bt_count ? 1 : 0
}

resource "google_project_service" "compute" {
  project                    = var.gcp_project_id
  service                    = "compute.googleapis.com"
  disable_dependent_services = var.disable_apis
  disable_on_destroy         = var.disable_apis
}

resource "google_project_service" "bigtable" {
  project                    = var.gcp_project_id
  service                    = "bigtable.googleapis.com"
  disable_dependent_services = var.disable_apis
  disable_on_destroy         = var.disable_apis
}

resource "google_project_service" "bigtableadmin" {
  project                    = var.gcp_project_id
  service                    = "bigtableadmin.googleapis.com"
  disable_dependent_services = var.disable_apis
  disable_on_destroy         = var.disable_apis
}
