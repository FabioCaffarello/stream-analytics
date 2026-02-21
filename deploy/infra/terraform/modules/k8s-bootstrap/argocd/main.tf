locals {
  template_vars = {
    deploy_root_app     = var.deploy_root_app
    environment         = var.environment
    namespace           = var.namespace
    argocd_root_project = var.argocd_root_project
    git_repo_url        = var.git_repo_url
    git_target_revision = var.git_target_revision
  }
}

resource "helm_release" "argocd" {
  name             = "argocd"
  chart            = "argo-cd"
  repository       = "https://argoproj.github.io/argo-helm"
  version          = "7.8.0"
  namespace        = var.namespace
  create_namespace = true

  values = [
    var.disable_enable_exec
    ? templatefile("${path.module}/values/argocd-no-exec.yaml.tftpl", local.template_vars)
    : templatefile("${path.module}/values/argocd.yaml.tftpl", local.template_vars)
  ]
}
