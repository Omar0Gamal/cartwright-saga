# Single-node k3s on GCP

For cloud deployment using Terraform, we opted for a single-node k3s cluster running on a small GCP GCE instance.

This setup is cheap, simple to provision, and sufficiently proves our Kubernetes deployment strategy. The obvious downside is the lack of high availability, but it perfectly fits our current demonstration requirements.
