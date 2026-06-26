# Terraform — Local Environment (minikube)

Provisions a local Kubernetes cluster via minikube and bootstraps ArgoCD.

## Prerequisites

- [Terraform](https://developer.hashicorp.com/terraform/install) >= 1.5
- [minikube](https://minikube.sigs.k8s.io/docs/start/) installed
- [Docker](https://docs.docker.com/get-docker/) running

## Quick Start

```bash
make tf-init    # terraform init
make tf-plan    # terraform plan
make tf-apply   # terraform apply
make tf-destroy # terraform destroy
```

## Cluster Sizing

Defaults are sized for the full MR stack (infra + 4 services + observability + K8s overhead):

| Variable       | Default          | Rationale                                    |
|---------------|------------------|----------------------------------------------|
| cluster_name  | stream-analytics   |                                              |
| nodes         | 1                | Single node for local dev                    |
| memory        | 12288 MB (12G)   | TSDB 1G + CH 2G + NATS 256M + apps 2G + K8s |
| cpus          | 6                | Enough for all services + system pods        |
| disk_size     | 40g              | Images + DB volumes + logs                   |

Override via `terraform.tfvars` (git-ignored):

```hcl
memory = "16384"
cpus   = 8
```

## What Gets Installed

| Component        | Namespace   | Access                 |
|-----------------|-------------|------------------------|
| ArgoCD          | argocd      | http://localhost:30080  |
| metrics-server  | kube-system | Built-in addon         |

## ArgoCD Access

After `tf-apply`, ArgoCD UI is available at `http://localhost:30080`.

Get the initial admin password:

```bash
kubectl -n argocd get secret argocd-initial-admin-secret -o jsonpath="{.data.password}" | base64 -d
```
