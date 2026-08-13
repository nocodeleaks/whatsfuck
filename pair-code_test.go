package whatsmeow

import (
	"context"
	"testing"

	waBinary "github.com/nocodeleaks/whatsfuck/binary"
	"github.com/nocodeleaks/whatsfuck/types/events"
	waLog "github.com/nocodeleaks/whatsfuck/util/log"
)

func TestClassifyPairCodeErrorRefreshRequired(t *testing.T) {
	parent := &waBinary.Node{
		Tag: "notification",
		Content: []waBinary.Node{{
			Tag: "link_code_companion_reg",
			Attrs: waBinary.Attrs{
				"stage":                "refresh_code",
				"force_manual_refresh": "true",
			},
		}},
	}

	event := classifyPairCodeError(parent, &ElementMissingError{
		Tag: "link_code_pairing_wrapped_primary_ephemeral_pub",
		In:  "notification",
	})

	if event.Reason != events.PairCodeErrorRefreshRequired {
		t.Fatalf("reason = %q, want %q", event.Reason, events.PairCodeErrorRefreshRequired)
	}
	if !event.Retryable || !event.ForceManualRefresh || event.Stage != "refresh_code" {
		t.Fatalf("unexpected refresh event: %#v", event)
	}
}

func TestTryHandleCodePairNotificationEmitsInvalidOrExpiredCode(t *testing.T) {
	client := &Client{Log: waLog.Noop}
	client.phoneLinkingCache.Store(&phoneLinkingCache{pairingRef: "pairing-ref"})

	var received *events.PairCodeError
	client.AddEventHandler(func(raw any) {
		received, _ = raw.(*events.PairCodeError)
	})

	parent := &waBinary.Node{
		Tag: "notification",
		Content: []waBinary.Node{{
			Tag: "link_code_companion_reg",
			Attrs: waBinary.Attrs{
				"stage": "primary_hello",
			},
			Content: []waBinary.Node{{
				Tag:     "link_code_pairing_ref",
				Content: []byte("pairing-ref"),
			}},
		}},
	}

	client.tryHandleCodePairNotification(context.Background(), parent)

	if received == nil {
		t.Fatal("expected PairCodeError event")
	}
	if received.Reason != events.PairCodeErrorInvalidOrExpiredCode {
		t.Fatalf("reason = %q, want %q", received.Reason, events.PairCodeErrorInvalidOrExpiredCode)
	}
	if !received.Retryable || received.Stage != "primary_hello" {
		t.Fatalf("unexpected pair code event: %#v", received)
	}
}

func TestClassifyPairCodeErrorDoesNotReportRestriction(t *testing.T) {
	parent := &waBinary.Node{
		Tag: "notification",
		Content: []waBinary.Node{{
			Tag:   "link_code_companion_reg",
			Attrs: waBinary.Attrs{"stage": "primary_hello"},
		}},
	}

	event := classifyPairCodeError(parent, &ElementMissingError{
		Tag: "link_code_pairing_wrapped_primary_ephemeral_pub",
		In:  "notification",
	})

	if event.Reason != events.PairCodeErrorInvalidOrExpiredCode {
		t.Fatalf("reason = %q, want a code-specific failure", event.Reason)
	}
}
