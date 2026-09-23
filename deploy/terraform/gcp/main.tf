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
}

resource "google_compute_firewall" "cartwright_allow_admin" {
  name    = "cartwright-allow-admin"
  network = google_compute_network.cartwright_net.name

  allow {
    protocol = "tcp"
    ports    = ["22", "6443"]
  }

  source_ranges = [var.trusted_cidr]
}

resource "google_compute_instance" "k3s_node" {
  name         = "cartwright-k3s"
  machine_type = "e2-micro"
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
  EOF

  tags = ["http-server", "https-server"]
}

output "instance_ip" {
  value = google_compute_instance.k3s_node.network_interface[0].access_config[0].nat_ip
}
