variable "cluster_name" {
  description = "Cluster name"
  type        = string
  default     = "staging-cluster"
}

variable "nodes" {
  description = "Number of cluster nodes"
  type        = number
  default     = 3
}

variable "memory" {
  description = "Memory per node in MB"
  type        = string
  default     = "8192"
}

variable "cpus" {
  description = "CPUs per node"
  type        = number
  default     = 2
}

variable "disk_size" {
  description = "Disk size per node"
  type        = string
  default     = "20g"
}

variable "sops_age_private_key" {
  description = "Age private key for SOPS decryption (generate with: age-keygen)"
  type        = string
  sensitive   = true
  default     = ""
}
