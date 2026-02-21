module "minikube" {
  source = "../../modules/k8s-local-minikube"

  cluster_name = var.cluster_name
  nodes        = var.nodes
  memory       = var.memory
  cpus         = var.cpus
  disk_size    = var.disk_size
}

module "argocd" {
  source = "../../modules/k8s-bootstrap/argocd"

  namespace = local.argocd_namespace
}

module "dashboard" {
  source = "../../modules/k8s-bootstrap/k8s-dashboard"

  namespace = local.dashboard_namespace

  providers = {
    kubernetes = kubernetes
    helm       = helm
  }
}
