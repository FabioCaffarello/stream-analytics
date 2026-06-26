variable "namespace" {
  description = "Namespace where ArgoCD will be installed"
  type        = string
  default     = "argocd"
}

variable "deploy_root_app" {
  description = "Whether to deploy the root ArgoCD Application that bootstraps GitOps"
  type        = bool
  default     = false
}

variable "environment" {
  description = "Deployment environment (local|staging|prod)"
  type        = string
  default     = "local"

  validation {
    condition     = contains(["local", "staging", "prod"], var.environment)
    error_message = "Environment must be one of: local, staging, prod."
  }
}

variable "disable_enable_exec" {
  description = "If true, use plugin configuration without --enable-exec (for hardened prod)"
  type        = bool
  default     = false
}

variable "argocd_root_project" {
  description = "Name of the ArgoCD AppProject to use for the root Application"
  type        = string
  default     = "gitops-root"
}

variable "git_repo_url" {
  description = "Git repository URL for ArgoCD to watch"
  type        = string
  default     = "https://github.com/stream-analytics/stream-analytics.git"
}

variable "git_target_revision" {
  description = "Git branch/tag/commit for ArgoCD to track"
  type        = string
  default     = "main"
}
