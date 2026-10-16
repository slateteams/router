package config

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCodexExtraCreditsOptIn(t *testing.T) {
	for _, tc := range []struct {
		raw           string
		want, invalid bool
	}{{"", false, false}, {"false", false, false}, {"true", true, false}, {" TRUE ", true, false}, {"typo", false, true}} {
		t.Run(tc.raw, func(t *testing.T) {
			t.Setenv("ROUTER_CODEX_ALLOW_EXTRA_CREDITS", tc.raw)
			got, err := CodexExtraCreditsAllowed()
			if tc.invalid {
				require.Error(t, err)
				require.False(t, got)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.want, got)
			}
		})
	}
}
