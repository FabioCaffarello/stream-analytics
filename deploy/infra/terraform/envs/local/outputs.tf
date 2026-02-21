output "cluster_host" {
  description = "Minikube cluster API server host"
  value       = module.minikube.cluster_host
}

output "argocd_url" {
  description = "ArgoCD UI (NodePort)"
  value       = "http://localhost:30080"
}
