package listen

import "testing"

// People type a local address several ways and all of them should work.
// Requiring a scheme for localhost is pointless friction, and getting this
// wrong means the CLI silently POSTs to the wrong place.
func TestNormaliseTarget(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"localhost:3000/webhooks", "http://localhost:3000/webhooks"},
		{":3000/webhooks", "http://localhost:3000/webhooks"},
		{"127.0.0.1:8080", "http://127.0.0.1:8080"},
		{"http://localhost:3000/hook", "http://localhost:3000/hook"},
		// An explicit https target is left alone — someone running a local TLS
		// proxy means it.
		{"https://local.test/hook", "https://local.test/hook"},
		{"  localhost:3000  ", "http://localhost:3000"},
	}

	for _, c := range cases {
		if got := normaliseTarget(c.in); got != c.want {
			t.Errorf("normaliseTarget(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
