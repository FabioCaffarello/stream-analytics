// Use 127.0.0.1 (IPv4) for Grafana: port 3000 may be shadowed by a local Node.js process
// on macOS where `localhost` resolves to ::1 (IPv6) first.
const ENDPOINTS = [
  { name: 'nginx/client',  url: 'http://localhost:8090/healthz' },
  { name: 'server',        url: 'http://localhost:8080/healthz' },
  { name: 'grafana',       url: 'http://127.0.0.1:3000/api/health' },
  { name: 'metabase',      url: 'http://127.0.0.1:3001/api/health' },
  { name: 'flink',         url: 'http://localhost:8091/overview' },
];

async function checkEndpoint(url: string): Promise<boolean> {
  try {
    const res = await fetch(url, { signal: AbortSignal.timeout(3_000) });
    return res.status < 400;
  } catch {
    return false;
  }
}

export async function waitForStack(timeoutMs = 180_000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  const healthy = new Set<string>();

  console.log('[wait-for-stack] waiting for all services to be healthy...');

  while (Date.now() < deadline) {
    for (const ep of ENDPOINTS) {
      if (healthy.has(ep.name)) continue;
      if (await checkEndpoint(ep.url)) {
        healthy.add(ep.name);
        console.log(`[wait-for-stack] ✓ ${ep.name} (${ep.url})`);
      }
    }

    if (healthy.size === ENDPOINTS.length) {
      console.log('[wait-for-stack] all services healthy');
      return;
    }

    const pending = ENDPOINTS.filter(e => !healthy.has(e.name)).map(e => e.name);
    console.log(`[wait-for-stack] waiting for: ${pending.join(', ')}`);
    await new Promise(r => setTimeout(r, 2_000));
  }

  const pending = ENDPOINTS.filter(e => !healthy.has(e.name)).map(e => e.name);
  throw new Error(`[wait-for-stack] timed out after ${timeoutMs}ms; still unhealthy: ${pending.join(', ')}`);
}
