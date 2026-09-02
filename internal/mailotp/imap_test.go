package mailotp

import "testing"

func TestFindOTPCode(t *testing.T) {
	cases := []struct {
		name  string
		texts []string
		want  string
	}{
		{
			name:  "code word in subject",
			texts: []string{"Your United verification code is 483920"},
			want:  "483920",
		},
		{
			name:  "year before the code in the same line",
			texts: []string{"Trip on Aug 10 2026 — your security code: 771044"},
			want:  "771044",
		},
		{
			name:  "stray number in subject, real code in body",
			texts: []string{"Your 2026 travel update", "Enter this one-time code: 552310 to continue."},
			want:  "552310",
		},
		{
			name:  "code precedes the wording",
			texts: []string{"", "902184 is your MileagePlus verification code."},
			want:  "902184",
		},
		{
			name:  "no keyword anywhere falls back to first digit run",
			texts: []string{"", "Hello, 4471 is what you need."},
			want:  "4471",
		},
		{
			name:  "no digits at all",
			texts: []string{"Welcome back", "Nothing to see here."},
			want:  "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := findOTPCode(c.texts...); got != c.want {
				t.Errorf("findOTPCode(%q) = %q, want %q", c.texts, got, c.want)
			}
		})
	}
}
