package templates

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStrftime(t *testing.T) {
	t.Parallel()

	ts := time.Date(2024, time.March, 5, 15, 4, 9, 0, time.UTC)
	for _, tc := range []struct{ format, want string }{
		{"%Y-%m-%d", "2024-03-05"},
		{"%B %d, %Y", "March 05, 2024"},
		{"%-d %b %Y", "5 Mar 2024"},
		{"%A %a", "Tuesday Tue"},
		{"%e|%y|%j", " 5|24|065"},
		{"%H:%M:%S %p %I", "15:04:09 PM 03"},
		{"%F %T", "2024-03-05 15:04:09"},
		{"%z %Z", "+0000 UTC"},
		{"Monthly 15 Jan MST", "Monthly 15 Jan MST"},
		{"100%% %Q", "100% %Q"},
		{"trailing %", "trailing %"},
	} {
		t.Run(tc.format, func(t *testing.T) {
			require.Equal(t, tc.want, strftime(ts, tc.format))
		})
	}
}
