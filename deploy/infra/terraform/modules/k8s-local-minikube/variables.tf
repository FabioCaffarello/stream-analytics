variable "cluster_name" {
  description = "Minikube cluster name"
  type        = string
}

variable "nodes" {
  description = "Number of cluster nodes"
  type        = number
}

variable "memory" {
  description = "Memory per node in MB"
  type        = string

  validation {
    condition     = can(tonumber(var.memory)) && tonumber(var.memory) >= 4096
    error_message = "Memory must be a numeric string >= 4096 (MB)."
  }
}

variable "cpus" {
  description = "CPUs per node"
  type        = number

  validation {
    condition     = var.cpus >= 2 && var.cpus <= 16
    error_message = "CPUs must be between 2 and 16."
  }
}

variable "disk_size" {
  description = "Disk size per node (e.g. 20g, 100g)"
  type        = string

  validation {
    condition     = can(regex("^[0-9]+[gG]$", var.disk_size))
    error_message = "Disk size must match pattern '<number>g' (e.g. 20g, 100g)."
  }
}
