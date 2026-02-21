module "k8s" {
  source = "../../modules/k8s-local-minikube"
  # staging should use real cluster module in real infra; this placeholder ensures argocd module is wired for now
  cluster_name = "staging-cluster"
  nodes        = 3
  memory       = "8192"
  cpus         = 2
  disk_size    = "20g"
}

module "argocd" {
  source = "../../modules/k8s-bootstrap/argocd"

  namespace           = "argocd"
  deploy_root_app     = true
  argocd_root_project = "gitops-root-staging"
  environment         = "staging"
  disable_enable_exec = false
}
