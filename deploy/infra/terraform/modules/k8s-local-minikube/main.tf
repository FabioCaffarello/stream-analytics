resource "minikube_cluster" "this" {
  driver       = "docker"
  cluster_name = var.cluster_name
  nodes        = var.nodes
  memory       = var.memory
  cpus         = var.cpus
  disk_size    = var.disk_size

  addons = [
    "default-storageclass",
    "storage-provisioner",
    "metrics-server",
  ]
}
