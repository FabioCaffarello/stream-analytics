---
slug: deployment
category: operations
generatedAt: 2026-02-12T02:18:17.155Z
relevantFiles:
  - Dockerfile
  - docker-compose.yml
  - .github/workflows/cd.yml
  - .github/workflows/ci.yml
  - .cache/go-mod/google.golang.org/protobuf@v1.32.0/.github/workflows/test.yml
  - .cache/go-mod/gopkg.in/yaml.v3@v3.0.1/.github/workflows/go.yaml
  - .cache/go-mod/github.com/!data!dog/gostackparse@v0.7.0/.github/workflows/go.yml
  - .cache/go-mod/github.com/anthdm/hollywood@v1.0.5/.github/workflows/build.yml
  - .cache/go-mod/github.com/google/go-cmp@v0.5.5/.github/workflows/test.yml
  - .cache/go-mod/github.com/google/uuid@v1.6.0/.github/workflows/apidiff.yaml
---

# How do I deploy this project?

## Deployment

### Docker

This project includes Docker configuration.

```bash
docker build -t app .
docker run -p 3000:3000 app
```

### CI/CD

CI/CD pipelines are configured for this project.
Check `.github/workflows/` or equivalent for pipeline configuration.