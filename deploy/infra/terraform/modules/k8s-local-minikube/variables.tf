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
}

variable "cpus" {
  description = "CPUs per node"
  type        = number
}

variable "disk_size" {
  description = "Disk size per node in MB"
  type        = string
}
