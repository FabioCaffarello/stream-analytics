resource "kubernetes_secret" "sops_age_key" {
  metadata {
    name      = "sops-age-key"
    namespace = var.namespace
    labels = {
      "app.kubernetes.io/part-of"    = "market-raccoon"
      "app.kubernetes.io/managed-by" = "terraform"
      "app.kubernetes.io/component"  = "sops"
    }
  }

  data = {
    "keys.txt" = var.age_private_key
  }

  type = "Opaque"
}
