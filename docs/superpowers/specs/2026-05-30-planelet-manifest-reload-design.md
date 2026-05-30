# Planelet Manifest Refresh — Design

Manual reload button + automatic refresh on action-picker open.

**Date:** 2026-05-30
**Status:** Approved, ready for implementation plan

## Problem

A Planelet integration's actions and triggers come from a manifest fetched
from the user's own Planelet server (`GET /manifest`). When the user updates
that server — adds an action, changes a parameter — SuperPlane keeps serving
the stale manifest until something happens to re-fetch it (a `Sync` on
reconnect, or opening the action picker which calls `ListResources`). There is
no explicit, user-driven way to say "re-read the manifest now."

Other integrations (GitHub, Slack, …) hardcode their actions and config fields
in Go. They have no remote manifest, so "reloading" them is meaningless. This
feature is Planelet-only by nature.

## Goal

Keep the displayed Planelet actions and parameters fresh without restarting
SuperPlane, via two complementary paths:

1. **Automatic** — re-fetch the manifest when the user opens the action picker,
   so newly added actions (and, on selection, their newly added parameters)
   appear during normal use. Gated by a long staleness window so rapid clicking
   does not hammer the Planelet server.
2. **Manual** — an explicit reload button (node sidebar + integration settings
   page) that force-fetches the manifest, bypassing the staleness window.

## Non-Goals

- Refreshing non-Planelet integrations. The RPC rejects them.
- Fixing the global manifest cache's multi-instance "last write wins"
  limitation. Pre-existing; explicitly deferred (see Known Limitations).
- Background polling / live push. Refresh is triggered by user interaction
  (dropdown open or button click) only.

## Backend

### New RPC: `RefreshIntegration`

On the Organization gRPC service (`protos/organizations.proto`):

```proto
rpc RefreshIntegration(RefreshIntegrationRequest) returns (RefreshIntegrationResponse) {
  option (google.api.http) = {
    post: "/api/v1/organizations/{id}/integrations/{integration_id}/refresh"
    body: "*"
  };
  option (grpc.gateway.protoc_gen_openapiv2.options.openapiv2_operation) = {
    summary: "Refresh an integration's manifest";
    description: "Re-fetches a Planelet integration's manifest and re-warms the catalog";
    tags: "Organization";
  };
}

message RefreshIntegrationRequest {
  string id = 1;             // organization id (path)
  string integration_id = 2; // installation id (path)
}

message RefreshIntegrationResponse {
  Integration integration = 1; // the refreshed integration, same shape as Describe
}
```

A dedicated response message (not a reuse of `DescribeIntegrationResponse`) so
the contract stays explicit and can diverge later if needed.

### Handler: `pkg/grpc/actions/organizations/refresh_integration.go`

Mirrors `list_integration_resources.go` for context construction.

1. Parse + validate `org` and `integration_id` UUIDs.
2. `models.FindIntegration(org, id)`. `NotFound` → `codes.NotFound`.
3. If `instance.State != IntegrationStateReady` → `codes.FailedPrecondition`
   ("integration is not ready").
4. If `instance.AppName != "planelet"` → `codes.FailedPrecondition`
   ("only Planelet integrations can be refreshed"). Honest scope — no
   pretending other integrations have a manifest.
5. Build `contexts.NewIntegrationContext(database.Conn(), nil, instance, …)`
   and `registry.HTTPContext()`.
6. Call `integration.Sync(core.SyncContext{…})` **synchronously**. Planelet's
   `Sync` is light — fetch manifest, cache it, set metadata, mark ready; no
   webhook side effects. Running it inline in the gRPC process re-warms the
   same in-memory cache that `Configuration()` reads for the catalog.
7. On Sync error: set `instance.State = error`, save, return
   `codes.FailedPrecondition` with the message.
8. On success: save instance, re-describe it, return in
   `RefreshIntegrationResponse`.

### Service wiring

Add the method to `OrganizationService` (`pkg/grpc/organization_service.go`),
delegating to the handler — same pattern as `ListIntegrations` /
`ListIntegrationResources`.

### Codegen

New RPC requires regenerating protobuf + gateway + OpenAPI + Go/TS clients:
`make pb.gen` (runs in the dev container; `make dev.up` first). Generated
artifacts are not committed (`.gitignore`), matching repo convention.

## Frontend

### Hook: `useRefreshIntegration(organizationId)`

In `web_src/src/hooks/useIntegrations.ts`. A `useMutation` calling the
generated `organizationsRefreshIntegration` SDK function. On success,
invalidate:

- `integrationKeys.available()` — the catalog, so action list + config fields
  re-fetch with the new manifest.
- `integrationKeys.integration(organizationId, integrationId)` — the instance.
- `integrationKeys.resources(organizationId, integrationId, …)` — the action /
  trigger pickers. (Invalidate by the resources key prefix.)

Surface `isPending` and error for button state + toast.

The hook accepts a `force` flag (default `true` for the manual button). When
`force`, it issues the refresh RPC unconditionally. When not forced (the
automatic path), it first checks staleness (see below) and no-ops if the
manifest is fresh.

### Automatic refresh on action-picker open

When the user opens the Planelet action picker (`IntegrationResourceFieldRenderer`
for `resourceType === "action"`, planelet-backed), fire a non-forced refresh
*before* showing the list, so newly added actions appear. On selecting an
action, its (now fresh) parameters flow in via the catalog.

**Staleness window — long.** Planelet servers rarely change mid-session, so we
do not want to re-fetch on every open. Track the last successful refresh time
per integration (a timestamp in a small client-side map, or React Query's
`dataUpdatedAt` on the resources query) and skip the auto-refresh if younger
than `MANIFEST_STALE_MS` (≈5 minutes). The manual button bypasses this entirely
(`force: true`).

This is purely a frontend gate. The backend RPC always does a real fetch when
called; the window only decides *whether* the automatic path calls it.

### Button A — Integration settings page

A reload icon button beside the Planelet integration's status card
(`SettingsTab.tsx` integration section, or the integration details route).
Rendered only when `integrationName === "planelet"`. Spinner while pending;
success/error toast.

### Button B — Node sidebar (`SettingsTab.tsx`)

A small reload icon next to the action picker, rendered only for
planelet-backed `Run Action` nodes (gate on the integration name of the node's
component). Same mutation. After success, the refreshed catalog flows through
`allComponentsByName` → `configurationFields`, so the action dropdown and
parameter fields update in place.

**Unsaved-edits guard:** `SettingsTab`'s prop-sync `useEffect` (the one that
resets `nodeConfiguration` from props when `configurationFields` changes) may
wipe in-progress edits when the refresh updates `configurationFields`. During
implementation, verify whether it does; if so, guard the reset so a manifest
refresh does not discard the user's current configuration. Fix only if it
bites.

## Data Flow

Manual (button, `force: true`):

```
User clicks reload
  → POST /…/integrations/{id}/refresh
  → handler validates (ready + planelet)
  → integration.Sync()  [synchronous, gRPC process]
      → fetch /manifest → setCachedManifest(manifest)
  → return refreshed Integration
  → frontend invalidates available() + instance + resources queries
  → catalog refetch → Configuration() reads warm cache → fresh fields
  → action dropdown / param fields re-render
```

Automatic (action picker opened, `force: false`):

```
User opens action picker
  → manifest younger than MANIFEST_STALE_MS?
      yes → no-op, show current list
      no  → same POST /…/refresh flow as above, then show fresh list
  → user picks an action → fresh params render
```

Both paths converge on the same RPC and the same query invalidations; they
differ only in the staleness gate in front of the call.

## Error Handling

- Non-ready / non-planelet → `FailedPrecondition`, surfaced as a toast.
- Planelet server unreachable during Sync → instance goes to `error` state,
  `FailedPrecondition` returned; toast shows the reason. The button stays
  available for retry.
- Frontend mutation error → toast; no query invalidation (stale data kept
  rather than cleared).

## Testing

- Handler unit test (`refresh_integration_test.go`): ready+planelet succeeds
  and re-caches; non-ready rejected; non-planelet rejected; unreachable server
  → error state + `FailedPrecondition`. Follow the existing
  `list_integration_resources` / `common_test.go` test setup.
- `go build ./pkg/... ./cmd/...`, `go vet`, package tests green.
- Frontend: `tsc --noEmit` clean; manual checks:
  - clicking the manual reload after editing the Planelet server's manifest
    surfaces a new action without a SuperPlane restart;
  - opening the action picker after a manifest change (and past the staleness
    window) surfaces the new action automatically;
  - opening the picker twice in quick succession only fetches once (staleness
    window holds).

## Known Limitations (deferred)

- **Global manifest cache.** `cachedManifest` is a process-global singleton.
  With multiple Planelet instances, refresh (like Sync/ListResources) writes
  the global, so `Configuration()` reflects whichever instance was cached last.
  Correct fix is keying the cache by integration ID and giving `Configuration()`
  instance context — out of scope here. Single-Planelet flows are unaffected.

## Affected Files

Backend:
- `protos/organizations.proto` — new RPC + messages
- `pkg/grpc/actions/organizations/refresh_integration.go` — handler (new)
- `pkg/grpc/actions/organizations/refresh_integration_test.go` — tests (new)
- `pkg/grpc/organization_service.go` — service method
- (generated artifacts via `make pb.gen`, not committed)

Frontend:
- `web_src/src/hooks/useIntegrations.ts` — `useRefreshIntegration` (with
  `force` flag + staleness gate / last-refresh tracking)
- `web_src/src/ui/configurationFieldRenderer/IntegrationResourceFieldRenderer.tsx`
  — auto-refresh on action-picker open (planelet, non-forced)
- `web_src/src/ui/componentSidebar/SettingsTab.tsx` — manual button (node
  sidebar) + unsaved-edits guard
- (integration settings page component — manual button A, if it lives separately)
