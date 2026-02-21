variable "namespace" {
  description = "Namespace for the Kubernetes Dashboard"
  type        = string
  default     = "kubernetes-dashboard"
}

variable "chart_version" {
  description = "Kubernetes Dashboard Helm chart version"
  type        = string
  default     = "7.12.0"
}
