package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/jetbrains/lior-cli/internal/i18n"
)

// FactorKind identifies an MFA factor type (MfaFactorVm.type).
type FactorKind string

// Supported MFA factor kinds.
const (
	FactorTOTP     FactorKind = "totp"
	FactorRecovery FactorKind = "recovery"
)

// Authenticator runs the guarded MFA step of the session flow. The connector
// must carry the session token (bearer) obtained by SignIn: challenge and
// verification endpoints are user-guarded (`POST /api/mfa/*`).
type Authenticator struct {
	Connector *Connector
}

// Prompt collects a verification code from the user for the given factor.
type Prompt func(factor FactorKind) (string, error)

// Verify checks MFA for the current session. It first asks the API for the
// available factors (POST /api/mfa/challenge); when no factor is required it
// returns (nil, nil) so callers can skip MFA. Otherwise it tries the enabled
// factors in challenge order (TOTP preferred), verifying the code through
// /api/mfa/totp/verify or /api/mfa/recovery/verify. A challenge failure is not
// swallowed: it is reported alongside the verification failure.
func (a *Authenticator) Verify(ctx context.Context, prompt Prompt) (*VerifyResponse, error) {
	challenge, challengeErr := a.Connector.ChallengeMFA(ctx)
	if challengeErr == nil {
		if !challenge.MFARequired {
			return nil, nil
		}
	}
	kinds := kindsFromFactors(challengeFactors(challenge, challengeErr))
	if len(kinds) == 0 {
		kinds = []FactorKind{FactorTOTP, FactorRecovery}
	}

	var lastErr error
	for _, kind := range kinds {
		code, err := prompt(kind)
		if err != nil {
			return nil, err
		}
		code = strings.TrimSpace(code)
		if code == "" {
			return nil, fmt.Errorf("%s", i18n.T("auth.error.mfa_code_required"))
		}

		switch kind {
		case FactorTOTP:
			resp, err := a.Connector.VerifyTOTP(ctx, code)
			if err != nil {
				lastErr = err
				continue
			}
			return resp, nil
		case FactorRecovery:
			resp, err := a.Connector.VerifyRecovery(ctx, code)
			if err != nil {
				lastErr = err
				continue
			}
			return resp, nil
		}
	}

	if lastErr == nil {
		return nil, fmt.Errorf("%s", i18n.T("auth.error.mfa_failed"))
	}
	if challengeErr != nil {
		return nil, fmt.Errorf("%s: %v; %s: %v",
			i18n.T("auth.error.mfa_failed"), lastErr,
			i18n.T("auth.error.mfa_challenge_failed"), challengeErr)
	}
	return nil, fmt.Errorf("%s: %v", i18n.T("auth.error.mfa_failed"), lastErr)
}

// challengeFactors returns the factors exposed by a successful challenge, or
// nil when the challenge failed (the fallback loop then covers TOTP + recovery).
func challengeFactors(challenge *ChallengeResponse, err error) []MFAFactor {
	if err != nil || challenge == nil {
		return nil
	}
	return challenge.Factors
}

// kindsFromFactors maps the API factors to the local supported kinds.
func kindsFromFactors(factors []MFAFactor) []FactorKind {
	var kinds []FactorKind
	for _, f := range factors {
		if !f.Enabled {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(f.Type)) {
		case "totp", "token", "otp", "authenticator":
			kinds = append(kinds, FactorTOTP)
		case "recovery", "backup", "recovery_code", "backup_code", "codes":
			kinds = append(kinds, FactorRecovery)
		}
	}
	return kinds
}
