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

  namespace           = local.argocd_namespace
  deploy_root_app     = true
  argocd_root_project = "gitops-root-local"
}

module "sops" {
  source = "../../modules/k8s-bootstrap/sops"

  age_private_key = var.sops_age_private_key
  namespace       = local.argocd_namespace

  depends_on = [module.argocd]
}
