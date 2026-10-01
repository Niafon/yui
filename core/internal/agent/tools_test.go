package agent

import (
	"testing"

	"github.com/yui-companion/core/internal/model"
)

func TestVoiceNeverSatisfiesHighRisk(t *testing.T) {
	// SEC-002 and R-07: a recorded or synthesised voice must not approve
	// anything above low risk.
	if !weaker(model.ConfirmVoice, model.ConfirmBiometric) {
		t.Fatal("voice must be rejected where biometry is required")
	}
	if !weaker(model.ConfirmVoice, model.ConfirmPIN) {
		t.Fatal("voice must be rejected where a PIN is required")
	}
	if !weaker(model.ConfirmButton, model.ConfirmPIN) {
		t.Fatal("a button press must not stand in for a strong factor")
	}
}

func TestStrongFactorsAreInterchangeable(t *testing.T) {
	// SRS 16.5 treats Face ID and PIN as alternative strong factors.
	if weaker(model.ConfirmPIN, model.ConfirmBiometric) {
		t.Fatal("a PIN should satisfy a biometric requirement")
	}
	if weaker(model.ConfirmBiometric, model.ConfirmPIN) {
		t.Fatal("biometry should satisfy a PIN requirement")
	}
}

func TestStrongerFactorThanRequiredIsAccepted(t *testing.T) {
	if weaker(model.ConfirmBiometric, model.ConfirmButton) {
		t.Fatal("a stronger factor than required must be accepted")
	}
}
