variable "age_private_key" {
  description = "Age private key for SOPS decryption (starts with AGE-SECRET-KEY-)"
  type        = string
  sensitive   = true

  validation {
    condition     = var.age_private_key == "" || can(regex("^AGE-SECRET-KEY-", var.age_private_key))
    error_message = "Age private key must start with 'AGE-SECRET-KEY-'."
  }
}

variable "namespace" {
  description = "Namespace where the SOPS age key secret will be created"
  type        = string
  default     = "argocd"
}
