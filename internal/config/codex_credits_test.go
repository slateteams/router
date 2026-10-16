package config_test

import (
	"github.com/stretchr/testify/require"
	"testing"
	"weave-os/router/internal/config"
)

func TestCodexExtraCreditsOptIn(t *testing.T) {
	for _, tc := range []struct {
		raw           string
		want, invalid bool
	}{{"", false, false}, {"false", false, false}, {"true", true, false}, {" TRUE ", true, false}, {"typo", false, true}} {
		t.Run(tc.raw, func(t *testing.T) {
			t.Setenv("ROUTER_CODEX_ALLOW_EXTRA_CREDITS", tc.raw)
			got, err := config.CodexExtraCreditsAllowed()
			if tc.invalid {
				require.ErrorContains(t, err, `"`+tc.raw+`"`)
				require.False(t, got)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.want, got)
			}
		})
	}
}
