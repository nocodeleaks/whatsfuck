// Copyright (c) 2026 Rajeh Taher
//
// Licensed under the MIT License. See LICENSE-MIT for details.

//go:build !benchmark_legacy

package main

import whatsmeow "github.com/nocodeleaks/whatsfuck"

func enablePhoneConsentReceiveBarrier(client *whatsmeow.Client) {
	//lint:ignore SA1019 The benchmark requires the internal receive barrier to measure completed protocol work.
	client.DangerousInternals().SetSynchronousMessageNameUpdates(true)
}
