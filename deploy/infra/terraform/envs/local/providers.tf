provider "minikube" {}

provider "helm" {
  kubernetes {
    host                   = module.minikube.cluster_host
    client_certificate     = module.minikube.cluster_client_certificate
    client_key             = module.minikube.cluster_client_key
    cluster_ca_certificate = module.minikube.cluster_ca_certificate
  }
}
