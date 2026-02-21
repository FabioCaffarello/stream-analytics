variable "cluster_name" {
  description = "Minikube cluster name"
  type        = string
  default     = "market-raccoon"
}

variable "nodes" {
  description = "Number of cluster nodes"
  type        = number
  default     = 1
}

variable "memory" {
  description = "Memory per node in MB"
  type        = string
  default     = "12288"
}

variable "cpus" {
  description = "CPUs per node"
  type        = number
  default     = 6
}

variable "disk_size" {
  description = "Disk size per node"
  type        = string
  default     = "40g"
}
