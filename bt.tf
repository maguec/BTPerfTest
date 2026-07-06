resource "google_bigtable_instance" "bt" {
  name                = "bt-i-${random_id.server.hex}"
  deletion_protection = false

  cluster {
    cluster_id   = "bt-${random_id.server.hex}"
    zone         = var.gcp_zone
    num_nodes    = var.bt_nodes
    storage_type = var.bt_storage
  }

  lifecycle {
    prevent_destroy = false
  }
}

# -------------------------------------------------------------------------
# Table 1: customere_charges
# -------------------------------------------------------------------------
resource "google_bigtable_table" "customere_charges" {
  name          = "customere_charges"
  instance_name = google_bigtable_instance.bt.name

  column_family { family = "core" }
  column_family { family = "customer" }
  column_family { family = "payment" }
  column_family { family = "fraud" }
  column_family { family = "shipping" }
  column_family { family = "subs" }
  column_family { family = "metadata" }
}

# GC Rule: keep latest version only for 'core' column family in customere_charges
resource "google_bigtable_gc_policy" "customere_charges_core_gc" {
  instance_name = google_bigtable_instance.bt.name
  table         = google_bigtable_table.customere_charges.name
  column_family = "core"

  max_version {
    number = 1
  }
}

# -------------------------------------------------------------------------
# Table 2: customer_customers
# -------------------------------------------------------------------------
resource "google_bigtable_table" "customer_customers" {
  name          = "customer_customers"
  instance_name = google_bigtable_instance.bt.name

  column_family { family = "core" }
  column_family { family = "financial" }
  column_family { family = "address" }
  column_family { family = "shipping" }
  column_family { family = "metadata" }
}

# GC Rule: keep latest version only for 'core' column family in customer_customers
resource "google_bigtable_gc_policy" "customer_customers_core_gc" {
  instance_name = google_bigtable_instance.bt.name
  table         = google_bigtable_table.customer_customers.name
  column_family = "core"

  max_version {
    number = 1
  }
}

# -------------------------------------------------------------------------
# Table 3: customer_merchant_daily_metrics_mv
# -------------------------------------------------------------------------
resource "google_bigtable_table" "customer_merchant_daily_metrics_mv" {
  name          = "customer_merchant_daily_metrics_mv"
  instance_name = google_bigtable_instance.bt.name

  column_family { family = "core" }
}

# GC Rule: keep latest version only for 'core' column family in the materialized view
resource "google_bigtable_gc_policy" "customer_merchant_daily_metrics_mv_core_gc" {
  instance_name = google_bigtable_instance.bt.name
  table         = google_bigtable_table.customer_merchant_daily_metrics_mv.name
  column_family = "core"

  max_version {
    number = 1
  }
}
