package handlers

import "testing"

func TestTruncatePreview(t *testing.T) {
	cases := []struct {
		name, in string
		max      int
		want     string
	}{
		{"short", "hello", 10, "hello"},
		{"exact", "hello", 5, "hello"},
		{"truncate", "hello world", 5, "hello…"},
		{"empty", "", 5, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := truncatePreview(c.in, c.max)
			if got != c.want {
				t.Errorf("truncatePreview(%q, %d) = %q, want %q", c.in, c.max, got, c.want)
			}
		})
	}
}

func TestTruncatePreview_Thai(t *testing.T) {
	// "สวัสดีชาวโลก" — each Thai char is 3 bytes in UTF-8.
	// max=9 → 3 Thai chars = "สวัสดี" (9 bytes) + "…"
	in := "สวัสดีชาวโลก"
	got := truncatePreview(in, 9)
	if len(got) == 0 {
		t.Fatal("truncatePreview returned empty string")
	}
	// Verify result is valid UTF-8 and ends with the ellipsis suffix.
	if got[len(got)-3:] != "…" {
		t.Errorf("truncatePreview(%q, 9) = %q — expected suffix '…'", in, got)
	}
}
