package bootstrap

import "testing"

func TestDeriveShardIndexFromHostname(t *testing.T) {
	tests := []struct {
		name string
		host string
		want int
		ok   bool
	}{
		{name: "compose ordinal 1", host: "compose-processor-1", want: 0, ok: true},
		{name: "compose ordinal 2", host: "compose-processor-2", want: 1, ok: true},
		{name: "k8s ordinal 0", host: "processor-0", want: 0, ok: true},
		{name: "invalid no suffix", host: "compose-processor", want: 0, ok: false},
		{name: "invalid empty", host: "", want: 0, ok: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := deriveShardIndexFromHostname(tc.host)
			if ok != tc.ok {
				t.Fatalf("ok=%v want=%v", ok, tc.ok)
			}
			if got != tc.want {
				t.Fatalf("index=%d want=%d", got, tc.want)
			}
		})
	}
}

func TestDeriveShardIndexFromComposeMetadata(t *testing.T) {
	composeContainerNumberProvider = func(containerID string) (int, bool) {
		if containerID != "13d5d7951762" {
			return 0, false
		}
		return 2, true
	}
	t.Cleanup(func() {
		composeContainerNumberProvider = defaultComposeContainerNumberProvider
	})

	got, ok := deriveShardIndexFromComposeMetadata("13d5d7951762")
	if !ok {
		t.Fatalf("expected compose metadata fallback to resolve")
	}
	if got != 1 {
		t.Fatalf("index=%d want=1", got)
	}
}
