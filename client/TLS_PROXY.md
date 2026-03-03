# Native TLS Path (Official)

The Web build uses browser WebSocket and supports `wss://` directly.

The native Odin client currently runs WebSocket over TCP (`ws://`) and does not terminate TLS itself. For production-like environments, use a local TLS sidecar/reverse proxy and point native to a local `ws://` endpoint.

## Recommended topology

- Native client: `ws://127.0.0.1:18080/ws`
- Sidecar/proxy: terminates TLS, validates server certificate chain, forwards to upstream `wss://.../ws`

This keeps the hot path bounded in-client and delegates certificate handling to mature TLS stacks.

## Minimal Caddy example

```caddyfile
:18080 {
  reverse_proxy https://api.example.com:8080 {
    header_up Host api.example.com
  }
}
```

Run Caddy locally, then configure client URL as `ws://127.0.0.1:18080/ws`.

## Minimal nginx stream example

```nginx
stream {
  upstream mr_upstream {
    server api.example.com:8080;
  }

  server {
    listen 127.0.0.1:18080;
    proxy_pass mr_upstream;
    proxy_ssl on;
    proxy_ssl_server_name on;
    proxy_ssl_name api.example.com;
  }
}
```

## Error behavior

- Native `wss://...` currently returns TLS-not-supported from the socket layer and is categorized upstream as `HandshakeFailed` for reconnect policy.
- Logs and diagnostics sanitize URL query strings and never print API key/JWT material.
