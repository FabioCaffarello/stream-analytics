# ── Stage 1: Builder ─────────────────────────────────────────────
# Ubuntu for glibc compatibility with Odin's bundled LLVM.
FROM ubuntu:22.04 AS builder

ARG ODIN_VERSION=dev-2026-02
ARG ODIN_ARCH=amd64

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        make \
        lld \
    && rm -rf /var/lib/apt/lists/*

RUN curl -fsSL \
        "https://github.com/odin-lang/Odin/releases/download/${ODIN_VERSION}/odin-linux-${ODIN_ARCH}-${ODIN_VERSION}.tar.gz" \
        -o /tmp/odin.tar.gz \
    && mkdir -p /opt/odin \
    && tar xzf /tmp/odin.tar.gz -C /opt/odin --strip-components=1 \
    && rm /tmp/odin.tar.gz \
    && ln -s /opt/odin/odin /usr/local/bin/odin

RUN odin version

WORKDIR /src
COPY client/ client/

RUN make -C client build-wasm
RUN test -s client/web/app.wasm && echo "app.wasm OK: $(wc -c < client/web/app.wasm) bytes"

# ── Stage 2: Runtime ─────────────────────────────────────────────
# nginx:alpine for minimal static file serving.
FROM nginx:1.27-alpine

RUN rm -rf /usr/share/nginx/html/* \
    && apk add --no-cache wget \
    && mkdir -p /var/cache/nginx/client_temp \
                /var/cache/nginx/proxy_temp \
                /var/cache/nginx/fastcgi_temp \
                /var/cache/nginx/uwsgi_temp \
                /var/cache/nginx/scgi_temp \
                /tmp/nginx \
    && chown -R nginx:nginx /var/cache/nginx \
                            /var/log/nginx \
                            /etc/nginx/conf.d \
                            /usr/share/nginx/html \
                            /tmp/nginx

COPY deploy/nginx/client.conf /etc/nginx/conf.d/default.conf

COPY --from=builder /src/client/web/index.html /usr/share/nginx/html/
COPY --from=builder /src/client/web/odin.js    /usr/share/nginx/html/
COPY --from=builder /src/client/web/app.wasm   /usr/share/nginx/html/

RUN chown -R nginx:nginx /usr/share/nginx/html \
    && rm -f /docker-entrypoint.d/10-listen-on-ipv6-by-default.sh \
    && sed -i 's|pid\s*/run/nginx.pid;|pid /tmp/nginx/nginx.pid;|' /etc/nginx/nginx.conf \
    && sed -i 's|^user .*;|# user directive removed for non-root runtime;|' /etc/nginx/nginx.conf

USER nginx

EXPOSE 8090

CMD ["nginx", "-g", "daemon off;"]
