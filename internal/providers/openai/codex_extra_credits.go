package openai

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"

	"weave-os/router/internal/providers"
)

func codexIncludedLimitReached(err error) bool {
	var upstream *providers.UpstreamErrorResponse
	return errors.As(err, &upstream) && upstream.Status == http.StatusTooManyRequests
}

// Credit metadata is read only after the operator opts in. Missing or invalid
// quota data and provider spend controls never become permission to spend.
func codexCreditsAvailable(raw, spend, reached json.RawMessage) bool {
	var credits struct {
		HasCredits *bool   `json:"has_credits"`
		Unlimited  bool    `json:"unlimited"`
		Balance    *string `json:"balance"`
	}
	if json.Unmarshal(raw, &credits) != nil {
		return false
	}
	if !credits.Unlimited {
		if credits.HasCredits == nil || !*credits.HasCredits || credits.Balance == nil || !positiveCreditBalance(*credits.Balance) {
			return false
		}
	}
	if len(reached) > 0 && string(reached) != "null" {
		var reason struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(reached, &reason) != nil || reason.Type != "rate_limit_reached" {
			return false
		}
	}
	if len(spend) > 0 && string(spend) != "null" {
		var control struct {
			Reached    *bool `json:"reached"`
			Individual *struct {
				Remaining *string `json:"remaining"`
			} `json:"individual_limit"`
		}
		if json.Unmarshal(spend, &control) != nil || control.Reached == nil || *control.Reached {
			return false
		}
		if control.Individual != nil && (control.Individual.Remaining == nil || !positiveCreditBalance(*control.Individual.Remaining)) {
			return false
		}
	}
	return true
}

func positiveCreditBalance(raw string) bool {
	balance, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	return err == nil && !math.IsNaN(balance) && !math.IsInf(balance, 0) && balance > 0
}
