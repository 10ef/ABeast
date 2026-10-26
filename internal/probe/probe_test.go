package probe

import "testing"

func TestFlag(t *testing.T) {
	if Flag("jp") != "🇯🇵" || Flag("") != "🏳" {
		t.Fatal(Flag("jp"))
	}
}
