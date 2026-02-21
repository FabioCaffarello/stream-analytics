output "namespace" {
  description = "Dashboard namespace"
  value       = kubernetes_namespace.dashboard.metadata[0].name
}
