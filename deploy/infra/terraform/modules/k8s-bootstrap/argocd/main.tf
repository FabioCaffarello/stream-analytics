resource "helm_release" "argocd" {
  name             = "argocd"
  chart            = "argo-cd"
  repository       = "https://argoproj.github.io/argo-helm"
  namespace        = var.namespace
  create_namespace = true

  values = [file("${path.module}/values/argocd.yaml")]
}
