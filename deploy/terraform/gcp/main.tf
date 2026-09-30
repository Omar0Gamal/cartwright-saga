terraform {
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 5.0"
    }
  }
}

provider "google" {
  project = var.project_id
  region  = var.region
  zone    = var.zone
}

variable "project_id" {
  type = string
}

variable "region" {
  type    = string
  default = "us-central1"
}

variable "zone" {
  type    = string
  default = "us-central1-a"
}

variable "trusted_cidr" {
  type        = string
  description = "CIDR block allowed to access SSH and the k3s API"
  default     = "127.0.0.1/32" # Default to loopback to force user to configure it safely
}

resource "google_compute_network" "cartwright_net" {
  name                    = "cartwright-network"
  auto_create_subnetworks = true
}

resource "google_compute_firewall" "cartwright_allow_http" {
  name    = "cartwright-allow-http"
  network = google_compute_network.cartwright_net.name

  allow {
    protocol = "tcp"
    ports    = ["80", "443"]
  }

  source_ranges = ["0.0.0.0/0"]
  target_tags   = ["http-server", "https-server"]
}

resource "google_compute_firewall" "cartwright_allow_admin" {
  name    = "cartwright-allow-admin"
  network = google_compute_network.cartwright_net.name

  allow {
    protocol = "tcp"
    ports    = ["22", "6443"]
  }

  source_ranges = [var.trusted_cidr]
  target_tags   = ["admin-server"]
}

resource "google_compute_instance" "k3s_node" {
  name         = "cartwright-k3s"
  machine_type = "e2-standard-2"
  zone         = var.zone

  boot_disk {
    initialize_params {
      image = "debian-cloud/debian-11"
    }
  }

  network_interface {
    network = google_compute_network.cartwright_net.name
    access_config {
      // Ephemeral IP
    }
  }

  metadata_startup_script = <<-EOF
    #!/bin/bash
    curl -sfL https://get.k3s.io | sh -
    
    # Wait for node to be ready and kubeconfig to be generated
    sleep 15
    cp /etc/rancher/k3s/k3s.yaml /home/ubuntu/kubeconfig
    chmod 644 /home/ubuntu/kubeconfig
  EOF

  tags = ["http-server", "https-server", "admin-server"]

  service_account {
    email  = google_service_account.k3s_sa.email
    scopes = ["cloud-platform"]
  }
}

output "instance_ip" {
  value = google_compute_instance.k3s_node.network_interface[0].access_config[0].nat_ip
}

resource "random_password" "postgres_password" {
  length           = 32
  special          = true
  override_special = "!#$%&*()-_=+[]{}<>:?"
}

resource "google_project_service" "secretmanager" {
  service = "secretmanager.googleapis.com"
  disable_on_destroy = false
}

resource "google_secret_manager_secret" "postgres_password" {
  secret_id = "cartwright-postgres-password"
  
  replication {
    auto {}
  }

  rotation {
    rotation_period = "2592000s" # 30 days
    next_rotation_time = "2026-11-01T00:00:00Z"
  }

  depends_on = [google_project_service.secretmanager]
}

resource "google_secret_manager_secret_version" "postgres_password" {
  secret      = google_secret_manager_secret.postgres_password.id
  secret_data = random_password.postgres_password.result
}

resource "google_service_account" "k3s_sa" {
  account_id   = "cartwright-k3s-sa"
  display_name = "Service Account for K3s Node"
}

resource "google_project_iam_member" "k3s_sa_secret_accessor" {
  project = var.project_id
  role    = "roles/secretmanager.secretAccessor"
  member  = "serviceAccount:${google_service_account.k3s_sa.email}"
}
