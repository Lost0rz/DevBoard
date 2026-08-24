package quota

import (
	"context"
	"fmt"
	"sort"
)

// DetectedAccount is the only account shape allowed to cross the product/UI
// boundary. AccountKey is irreversible; source credentials and CodexBar
// identity fields are intentionally not represented here.
type DetectedAccount struct {
	Provider     string `json:"provider"`
	AccountKey   string `json:"accountKey"`
	DisplayLabel string `json:"displayLabel"`
	SourceHealth string `json:"sourceHealth"`
}

// Detection contains independent provider health. A CodexBar failure for one
// provider never hides a healthy result for the other provider.
type Detection struct {
	Accounts []DetectedAccount `json:"accounts"`
	Sources  map[string]string `json:"sources"`
}

// DetectAccounts performs bounded, read-only CodexBar probes for Codex and
// Z.ai/GLM. It never writes CodexBar or account state and never returns raw
// provider output.
func DetectAccounts(ctx context.Context, runner Runner, identityKey []byte, aliases map[string]string) (Detection, error) {
	if runner == nil {
		return Detection{}, fmt.Errorf("quota detection requires a runner")
	}
	detection := Detection{Accounts: []DetectedAccount{}, Sources: map[string]string{}}
	for _, provider := range []struct {
		name  string
		label string
	}{{name: "codex", label: "Codex"}, {name: "zai", label: "GLM"}} {
		callCtx, cancel := context.WithTimeout(ctx, CommandTimeout)
		body, err := runner.Run(callCtx, "codexbar", "usage", "--provider", provider.name, "--all-accounts", "--format", "json")
		cancel()
		if err != nil {
			detection.Sources[provider.name] = "unavailable"
			continue
		}
		accounts, err := parseAccounts(body, provider.name)
		if err != nil {
			// A valid empty array is handled by parseAccounts as ErrNoAccounts
			// and remains unavailable. No empty provider may look healthy.
			detection.Sources[provider.name] = "unavailable"
			continue
		}
		health := "available"
		if provider.name == "zai" && len(accounts) > 1 {
			health = "configuration_required"
		}
		providerAccounts := make([]DetectedAccount, 0, len(accounts))
		for _, account := range accounts {
			key := AccountKey(identityKey, provider.name, account.Identity)
			label := provider.label
			accountHealth := health
			if provider.name == "codex" {
				label = aliases[key]
				if label == "" {
					accountHealth = "configuration_required"
				}
			}
			providerAccounts = append(providerAccounts, DetectedAccount{
				Provider: provider.name, AccountKey: key, DisplayLabel: label, SourceHealth: accountHealth,
			})
		}
		detection.Accounts = append(detection.Accounts, providerAccounts...)
		detection.Sources[provider.name] = health
	}
	sort.SliceStable(detection.Accounts, func(i, j int) bool {
		if detection.Accounts[i].Provider != detection.Accounts[j].Provider {
			return detection.Accounts[i].Provider < detection.Accounts[j].Provider
		}
		return detection.Accounts[i].AccountKey < detection.Accounts[j].AccountKey
	})
	return detection, nil
}
