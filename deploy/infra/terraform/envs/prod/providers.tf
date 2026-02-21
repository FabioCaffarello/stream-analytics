provider "minikube" {}

provider "helm" {
  kubernetes {
    host                   = module.k8s.cluster_host
    client_certificate     = module.k8s.cluster_client_certificate
    client_key             = module.k8s.cluster_client_key
    cluster_ca_certificate = module.k8s.cluster_ca_certificate
  }
}

provider "kubernetes" {
  host                   = module.k8s.cluster_host
  client_certificate     = module.k8s.cluster_client_certificate
  client_key             = module.k8s.cluster_client_key
  cluster_ca_certificate = module.k8s.cluster_ca_certificate
}
