# Example: checkout service migration

**Status:** Draft<br>
**Last updated:** 2026-08-17<br>
**Visual profile:** Migration

## Executive summary

Move checkout authorization and order finalization from the monolithic Checkout API into a versioned Checkout Core service behind the existing checkout edge. The migration uses contract parity, shadow reads, and a narrow percentage cutover so customer-visible behavior remains stable while the new service takes ownership.

The plan is intentionally staged: first establish a compatible contract, then mirror production requests, then move reads, and finally move writes after rollback is proven. The current checkout edge remains the public boundary throughout.

## Destination

At completion, the checkout edge routes authorization and finalization to Checkout Core, while the existing monolith remains available as the rollback target until the stabilization window closes. Customers keep the existing checkout URL, payment-method behavior, and order-confirmation semantics.

Completion means that Checkout Core handles 100% of eligible checkout traffic for 48 hours with no material parity mismatch, no increase in payment failure rate, and a rehearsed rollback that restores monolith writes within ten minutes.

## Current state and verified evidence

- The checkout edge sends cart validation, payment authorization, and order finalization to the monolithic Checkout API.
- Payment-provider calls are synchronous; provider idempotency keys are created in the monolith and persisted with the order attempt.
- The order database has a single writer. Checkout Core has no production write permission.
- The existing contract tests cover successful card checkout and duplicate-submit handling, but not provider timeout recovery or partial order persistence.
- Synthetic baseline evidence for this plan is a 30-day sample: 99.94% checkout success, p95 authorization latency of 820 ms, and 0.18% duplicate-submit rate.

These figures are planning inputs, not a claim about a live migration. The first phase must replace them with a dated production baseline before a traffic decision is made.

## Settled decisions

- Keep the checkout edge as the stable public route; do not change customer-facing URLs during this migration.
- Use a strangler sequence of contract parity, shadow reads, read cutover, and write cutover rather than a single switch.
- Preserve the monolith as the rollback writer until the 48-hour stabilization window is complete.
- Make payment and order operations idempotent by carrying the existing attempt key through both services.
- Pause the rollout on any unexplained payment failure increase, order-state divergence, or provider timeout regression.

## Scope and non-goals

### In scope

- Checkout Core API for authorization, finalization, and retry-safe order state transitions.
- Edge routing, request correlation, dual-read comparison, dashboards, and rollback controls.
- Contract, provider-timeout, duplicate-submit, reconciliation, and cutover verification.

### Non-goals

- Replacing the payment provider or changing payment-method eligibility.
- Redesigning the checkout UI or changing pricing, tax, promotion, or inventory rules.
- Splitting the order database into new storage during this migration.
- Removing the monolith before the stabilization and reconciliation review.

## Requirements and proposed design

### Requirements

1. Preserve the existing edge request and response contract for all supported checkout clients.
2. Carry a stable correlation ID and payment attempt key through edge, Checkout Core, provider, and order records.
3. Compare normalized responses during shadow reads without issuing a second payment authorization.
4. Provide an operator-controlled percentage flag with an immediate monolith rollback path.
5. Reconcile every order attempt after each traffic increase before proceeding to the next gate.

### Proposed design

Checkout Core owns orchestration and state-transition decisions, but the existing order database remains the writer until the write cutover gate. The edge selects the target by account-safe rollout flag. Shadow mode replays read-only cart and order-state requests; authorization is observed through normalized provider outcomes rather than duplicated.

The write cutover uses a short dual-write handoff: Checkout Core records an outbox event and the monolith confirms the legacy order projection until reconciliation proves parity. Once the agreed mismatch threshold is met, the flag moves to Checkout Core-only writes. Rollback disables the flag and drains in-flight Core attempts before restoring monolith ownership.

## Ordered implementation phases

### Phase 0 — Baseline and contract lock

Capture the production baseline, freeze the edge contract, and add fixtures for provider timeout, duplicate submit, and partial persistence. This phase is complete only when the baseline dashboard and contract diff are reviewed.

### Phase 1 — Build parity and shadow mode

Implement Checkout Core behind an internal route, deploy read-only shadow comparison, and measure normalized response mismatches for one full business day. No customer payment is sent twice.

### Phase 2 — Move reads and rehearse rollback

Route 10%, 25%, and then 50% of eligible read traffic to Checkout Core. Run the rollback drill at 10% before increasing exposure. Reconcile cart, authorization status, and order projection results after each step.

### Phase 3 — Move writes and stabilize

At an approved window, route 10% of writes to Core, then 50%, then 100% after each gate passes. Keep the monolith writer-ready for 48 hours, complete reconciliation, and record the decommission decision separately.

## Acceptance and verification criteria

- Contract parity: all supported clients receive equivalent success, validation, and retry responses.
- Reliability: checkout success does not fall by more than 0.10 percentage points from the dated baseline during a gate.
- Performance: authorization p95 remains below 900 ms and provider timeout recovery does not regress.
- Integrity: zero unexplained duplicate charges, orphaned orders, or order-state divergences in reconciliation.
- Rollback: the on-call operator can return eligible writes to the monolith within ten minutes during a rehearsal.
- Stabilization: 100% eligible traffic remains on Core for 48 hours before the migration is declared complete.

## Risks and mitigations

- **Duplicate authorization:** A retry could reach both services. Carry one attempt key, prohibit shadow authorization, and alert on provider idempotency conflicts.
- **Order divergence:** Core and the monolith may interpret a transition differently. Compare normalized state transitions and block percentage increases on any unexplained mismatch.
- **Rollback gap:** In-flight Core writes may not be visible to the monolith immediately. Drain requests, replay the outbox, and verify reconciliation before restoring writes.
- **Provider timeout sensitivity:** New network hops may increase latency. Measure p95/p99 at each gate and keep the monolith route available for rollback.

## Non-blocking open questions

- Which on-call role owns the final 100% write-cutover decision during the approved window?
- Should the 48-hour stabilization window include low-volume weekend traffic before the monolith decommission review?
