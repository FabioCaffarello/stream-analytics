package services

// Small fixed-size percentile helpers for hot paths.
// - Pure, deterministic, no heap allocations.
// - Designed for ring buffers with bounded capacities.

@(private = "file")
clamp_percentile :: proc(pct: int) -> int {
	if pct < 0 do return 0
	if pct > 100 do return 100
	return pct
}

ring_percentile_i64 :: proc(samples: [$N]i64, head: int, count: int, pct: int) -> i64 {
	n := count
	if n <= 0 do return 0
	if n > N do n = N
	p := clamp_percentile(pct)
	start := (head - n + N) % N
	sorted: [N]i64
	for i in 0 ..< n {
		sorted[i] = samples[(start + i) % N]
	}
	for i in 1 ..< n {
		key := sorted[i]
		j := i - 1
		for j >= 0 && sorted[j] > key {
			sorted[j + 1] = sorted[j]
			j -= 1
		}
		sorted[j + 1] = key
	}
	return sorted[min((n * p) / 100, n - 1)]
}
