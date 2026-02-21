output "cluster_host" {
  description = "Cluster API server host"
  value       = minikube_cluster.this.host
}

output "cluster_client_certificate" {
  description = "Client certificate for cluster authentication"
  value       = minikube_cluster.this.client_certificate
}

output "cluster_client_key" {
  description = "Client key for cluster authentication"
  value       = minikube_cluster.this.client_key
  sensitive   = true
}

output "cluster_ca_certificate" {
  description = "CA certificate of the cluster"
  value       = minikube_cluster.this.cluster_ca_certificate
}
