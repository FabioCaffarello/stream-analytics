terraform {
  required_version = ">= 1.5"

  required_providers {
    minikube = {
      source  = "scott-the-programmer/minikube"
      version = "0.5.0"
    }
    helm = {
      source  = "hashicorp/helm"
      version = "2.17.0"
    }
  }

  backend "local" {
    path = "./terraform.tfstate"
  }
}
