module "k8s" {
  source = "../../modules/k8s-local-minikube"
  # staging should use real cluster module in real infra; this placeholder ensures argocd module is wired for now
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
  argocd_root_project = "gitops-root-staging"
  environment         = "staging"
  disable_enable_exec = false
}

module "sops" {
  source = "../../modules/k8s-bootstrap/sops"

  age_private_key = var.sops_age_private_key
  namespace       = local.argocd_namespace

  depends_on = [module.argocd]
}
