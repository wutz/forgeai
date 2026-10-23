package app

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
)

// TestAssessMessageKeys checks that the diagnostics the GUI words in the user's
// language carry a key and the values it fills in.
func TestAssessMessageKeys(t *testing.T) {
	bundled := Dependency{Name: "kueue", Pinned: "v0.20.2", Installable: true}

	partial := bundled
	assess(&partial, depHealth{crdsWanted: 3, crdsFound: 1})
	if partial.MessageKey != "crdsInstalled" || partial.MessageArgs["found"] != "1" || partial.MessageArgs["wanted"] != "3" {
		t.Errorf("CRD diagnostic = key %q args %v, want crdsInstalled with found 1 and wanted 3",
			partial.MessageKey, partial.MessageArgs)
	}
	if partial.Message != "1 of 3 CRDs installed" {
		t.Errorf("CRD diagnostic English = %q, want %q", partial.Message, "1 of 3 CRDs installed")
	}

	notReady := bundled
	assess(&notReady, depHealth{crdsWanted: 2, crdsFound: 2,
		deployments: []appsv1.Deployment{testDeployment("v0.20.2", false)}})
	if notReady.MessageKey != "notAvailable" || notReady.MessageArgs["deployments"] == "" {
		t.Errorf("availability diagnostic = key %q args %v, want notAvailable with the deployments",
			notReady.MessageKey, notReady.MessageArgs)
	}

	ready := bundled
	assess(&ready, depHealth{crdsWanted: 2, crdsFound: 2,
		deployments: []appsv1.Deployment{testDeployment("v0.20.2", true)}})
	if ready.MessageKey != "" || ready.MessageArgs != nil {
		t.Errorf("ready component carries diagnostic key %q args %v, want none", ready.MessageKey, ready.MessageArgs)
	}
}
