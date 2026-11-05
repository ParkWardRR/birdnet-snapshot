package main

import (
	"errors"
	"testing"
)

func TestIsCrash(t *testing.T) {
	crashes := []string{
		"playwright: Page crashed",
		"Target crashed",
		"Target closed",
		"frame has been closed",
	}
	for _, s := range crashes {
		if !isCrash(errors.New(s)) {
			t.Errorf("isCrash(%q) = false, want true", s)
		}
	}
	// A site that is merely slow or missing must not look like a dead process,
	// or the retry would paper over a real fault.
	notCrashes := []string{
		"Timeout 45000ms exceeded",
		"net::ERR_CONNECTION_REFUSED",
		"NS_ERROR_UNKNOWN_HOST",
	}
	for _, s := range notCrashes {
		if isCrash(errors.New(s)) {
			t.Errorf("isCrash(%q) = true, want false", s)
		}
	}
}

func TestCrashSeverity(t *testing.T) {
	// A crash that loaded on a second visit is worth seeing but must not fail
	// the scheduled check; one that did not is a plain navigation failure.
	if got := severity(finding{Kind: "page crashed", Detail: "recovered on a second visit: Page crashed"}); got != "warn" {
		t.Errorf("recovered crash severity = %q, want warn", got)
	}
	if got := severity(finding{Kind: "navigation", Detail: "playwright: Page crashed"}); got != "fail" {
		t.Errorf("unrecovered crash severity = %q, want fail", got)
	}
}
