package resources

import "testing"

// The table is generated, so these guard the generator rather than the data.
// A malformed group name or a missing method reaches the user as a command
// that cannot be typed or cannot run.
func TestEveryCommandIsUsable(t *testing.T) {
	if len(Commands) == 0 {
		t.Fatal("no commands generated: the spec did not parse")
	}
	for _, c := range Commands {
		if c.Group == "" || c.Verb == "" {
			t.Errorf("unusable command: %+v", c)
		}
		if c.Method == "" || c.Path == "" {
			t.Errorf("command cannot be executed: %+v", c)
		}
		for _, r := range c.Group + c.Verb {
			if r >= 'A' && r <= 'Z' {
				t.Errorf("%s %s: names must be lowercase to be typeable", c.Group, c.Verb)
				break
			}
		}
	}
}

// "checkout--sessions" is what you get from running a kebab-case conversion
// over a name that already contains a separator. It is unguessable and
// therefore untypeable.
func TestNoDoubledSeparators(t *testing.T) {
	for _, c := range Commands {
		for i := 1; i < len(c.Group); i++ {
			if c.Group[i] == '-' && c.Group[i-1] == '-' {
				t.Errorf("group %q has a doubled hyphen", c.Group)
				break
			}
		}
	}
}

func TestFindResolvesAKnownCommand(t *testing.T) {
	if _, ok := Find("products", "list"); !ok {
		t.Error("products list should resolve")
	}
	if _, ok := Find("products", "frobnicate"); ok {
		t.Error("an unknown verb must not resolve")
	}
}
