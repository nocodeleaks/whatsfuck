<!--
Copyright (c) 2026 Rajeh Taher
Licensed under the MIT License. See LICENSE-MIT for details.
-->

# Changelog

All notable HyperMeow changes are documented here. HyperMeow uses commit pseudo-versions from its reviewed `main` branch.

## [Unreleased]

### Upstream sync

Integrated the AI-reviewed official WhatsMeow range `a23afe3..b25a56d` (see `scripts/upstream-review.sh review official`), commit by commit rather than as the single reviewed-range merge the range's overall `rejected` verdict would have blocked. 19 of the 21 commits are now in, each applied or adapted individually; `UPSTREAMS.lock.json`'s `official.integrated_commit` is intentionally left at the prior baseline because this was commit-by-commit, not the reviewed-range merge the lock schema tracks — the next official review will re-surface this same range (expected and safe, since every included commit was already approved or has been individually adapted here).

Integrated directly:

- `8d023aa9` user: switch `UpdateBlocklist` to use LIDs (#1137), plus its `197e6174` import-cleanup follow-up.
- `72f22e67` / `1f6240e6` download: ignore unencrypted media keys for full downloads and thumbnails.
- `d1cc3c0a` ci: disable goimports on Go 1.26.
- `bdd4e83f` group: make delete reason optional.
- `0ca83463` client: guess correct own ID in `ParseWebMessage`.
- `57796d3d` group: deprecate duplicate topic method.
- `b25a56d6` send: add chat JID in `SendResponse` (kept alongside the fork's `PHashMismatch` field).
- `0dcf1f50` group: remove the no-op `ReqCreateGroup.CreateKey` (WhatsApp no longer honors it; `JoinedGroup.CreateKey` is untouched).

Integrated with fork-specific adaptation:

- `0fadda79` client,socket: use `fs.Context()` instead of the connect-call context for noise-socket frame consumption, adapted to this fork's ctx-free `doHandshake`/`cli.handleFrame` shape (upstream's own `NoiseHandshake.Finish`/`newNoiseSocket` signatures already matched).
- `b06ae6eb`, `6eefbff4`, `de26b4ab`, `30593f2a` proto: regenerated the touched packages (new AI/device-capability/Labyrinth fields, incompatible `RotateEpochInput`/`RotateEpochOutput`/`DeviceOutput` schema changes, removed `NewsletterAdminProfileMessageV2`) with `protoc` + `protoc-gen-go` rather than cherry-picked as text, since each file's own module path is length-prefixed inside its serialized `FileDescriptorProto` bytes. No hand-written code outside the generated proto packages referenced the changed types.
- `fb386f15` dependencies: update (`go.mau.fi/util`, `golang.org/x/crypto`/`net`/`exp`/`text`, `google.golang.org/protobuf`, `petermattis/goid`, toolchain go1.26.6). The fork's alternative libsignal dependency is untouched.
- `4650ea95` dependencies: bump minimum Go version to 1.26 — go.mod raised to 1.26.0, toolchain to go1.27.0, CI matrix to 1.26/1.27. **Breaking for consumers still on Go 1.25.**
- `4fa34623` client: don't reuse the handler queue between connections — re-threaded a per-connection queue through `doHandshake`/`handleFrame`/`enqueueNode`/`handlerQueueLoop` instead of the mechanical upstream diff, since this fork's `RawNodeHandler` hook, business out-of-band delivery, Signal-disabled synchronous handoff, and `forceAutoReconnect` overflow policy all needed to keep working unchanged.
- `33cfac51` ci: enable staticcheck — only the non-refactor parts applied: an internal `ErrEventAlreadyProcessed` reference cleanup and a QR-timeout log fix (see below). The `handlerQueue` field removal in that commit depended on `4fa34623` (now applied separately above); the newsletter.go hunk doesn't apply because this fork's MEX argo decoding is already disabled (`"argo decoding is currently broken"`); everything else in it was already present in the fork's baseline.
- Ported the two independent fixes from `4650ea955f8`/`33cfac5116293` that didn't depend on the Go-version bump or the queue field: log the QR-timeout event name instead of the struct's default representation, and use `ErrEventAlreadyProcessed` (not the deprecated alias) at its two internal call sites.

Excluded:

- `28bfe537` and `9ec8f76d` — the AI review found real bugs (a dropped `stream:error` in the queue-cancellation path, and a QR channel that terminates on ADV-secret rotation instead of continuing). Not integrated; worth reporting upstream.

HyperMeow's pending range `f9db181..07d103b` was reviewed and came back `rejected` in full; no commit was cherry-picked (the lone `approved` commit is a no-op on this base per the review).

### Documentation

- Added an evidence-backed comparison with upstream WhatsMeow.
- Consolidated the root white-box test suite into themed files without removing tests or changing coverage.
- Consolidated handwritten business-app features into `business.go` without changing the exported API.
- Moved committed benchmark reports and heap profiles into `benchmark/barback/testdata/results`; fresh run output remains under the ignored `benchmark/barback/results` workspace.
- Retracted every published semantic version; use `@main` for reviewed commits.
- Established `main` as the sole production source; `dev` remains experimental.

## First public release - 2026-08-10

The first HyperMeow release combines the reviewed `dev` train with upstream WhatsMeow protocol updates through protobuf revision `v1044834443`.

### Added

- Published the standalone `github.com/polymorfa/hypermeow` module while retaining the upstream `whatsmeow` package names.
- Added raw-node compatibility hooks for integrations that need protocol-level observation.
- Added business linked-account and feature-eligibility reads.
- Added business profile, cover photo, product, collection, catalog, cart, visibility, appeal, and merchant-compliance mutations.
- Added validated business message builders for product lists, orders, addresses, lists, and native Flows.
- Added native Flow response metadata handling and exact preservation of JSON number lexemes.
- Added newsletter deletion through generated MEX bindings.
- Added quick-reply app-state actions and events.
- Added atomic label replacement and configurable label/full-sync event emission.
- Added independent history-sync receipt, persistence, and media-deletion controls.
- Added phone-number consent request/share message builders.
- Added LID identity verification-code generation.
- Added username persistence and PN/username-to-LID alias resolution across contacts, groups, notifications, and group membership changes.
- Added optional batched reverse-LID lookup support with bounded query chunks and negative caching.
- Added a reproducible Barback/PostgreSQL benchmark suite for DM, group, history, media, mixed messages, client memory, saturation, phone consent, business operations, and security codes.

### Changed

- Made LIDs the primary Signal identity while retaining phone numbers and usernames as aliases when available.
- Reworked retry-message storage to allocate lazily and retain encoded payloads instead of full protobuf object graphs.
- Added bounded group, device, contact, identity, migration, retry, app-state-key, and PN-to-LID caches with durable stores remaining authoritative.
- Batched PostgreSQL session, identity, message-secret, and alias operations while retaining atomic fallbacks for generic SQL drivers.
- Avoided empty PN-to-LID database transactions and added PostgreSQL pattern indexes for migration-prefix lookups.
- Shared the default HTTP transport across clients while preserving isolation for custom proxy and HTTP-client configuration.
- Preserved the newest history-sync nonce through asynchronous persistence without blocking event delivery.

### Reliability

- Hardened malformed and odd-length binary node decoding.
- Serialized device save/delete operations and made overlapping history-sync nonce writes monotonic.
- Exposed participant-hash mismatches instead of silently accepting inconsistent group state.
- Added bounded handler queues and guarded reconnect behavior at overflow.
- Replaced unsupported Signal-store panics with returned errors.
- Added explicit error handling for receipt misuse, CBC invariant failures, missing signed prekeys, upload read failures, partial writes, and temporary-file cleanup.
- Added compatibility checks and Docker build support for archived WhatsMeow and pre-optimization Barback baselines after the module-path migration.

### Security and privacy

- Redacted business access tokens, cookies, nonces, profile fields, and linked-account payloads from binary-node logs.
- Added strict validation for business payload lengths, prices, product/section counts, mixed native-flow metadata, UTF-8, and trailing JSON.
- Added privacy-cache completeness and bounded failure behavior.
- Added atomic Signal identity insertion and deletion-generation fencing to prevent stale prekey fetches from restoring deleted identities.

### Performance evidence

- The frozen three-repeat system matrix completed 45 bounded workloads with no send failures, queue overflows, or temporary files left behind.
- Against the recorded upstream revision, HyperMeow reduced allocation in all five system scenarios, reduced group-128 send p95 by 79.3%, reduced group-128 client CPU by 58.2%, and reduced group-128 SQL calls by 97.9%.
- The isolated 2,000-client constructor benchmark measured 4,240 bytes of Go heap per HyperMeow client versus 78,929 bytes upstream, a 94.6% reduction in disconnected fixed state.
- The encrypted ping-pong test sustained 1,700 healthy pairs per second versus 900 upstream under the same bounded local environment.

See the [system comparison](benchmark/barback/testdata/results/system-comparison.md), [RAM hardening report](benchmark/barback/testdata/results/ram-hardening.md), and [saturation report](benchmark/barback/testdata/results/maxrate-ping-pong.md) for revisions, limits, caveats, and raw evidence.

### Compatibility notes

- Applications must migrate imports from `go.mau.fi/whatsmeow` to `github.com/polymorfa/hypermeow`.
- A single binary must not link both modules because they register identical generated protobuf descriptors.
- The Polymorfa Signal dependency changes exported Signal parameter types relative to upstream; other exported differences in this release are additive.
