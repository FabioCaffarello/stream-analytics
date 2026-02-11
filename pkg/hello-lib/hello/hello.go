// Package hello exposes greeting helpers used by workspace apps.
package hello

import "fmt"

// Message returns a normalized hello-world style greeting.
func Message(name string) string {
	if name == "" {
		name = "world"
	}

	return fmt.Sprintf("hello %s", name)
}
