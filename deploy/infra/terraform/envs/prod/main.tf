module "k8s" {
  source = "../../modules/k8s-local-minikube"
  # production should use the real cloud k8s module; this placeholder is intentionally minimal and idempotent
  cluster_name = "prod-cluster"
  nodes        = 3
  memory       = "16384"
  cpus         = 4
  disk_size    = "50g"
}

module "argocd" {
  source = "../../modules/k8s-bootstrap/argocd"

  namespace           = "argocd"
  deploy_root_app     = true
  argocd_root_project = "gitops-root-prod"
  environment         = "prod"
  disable_enable_exec = true
}
