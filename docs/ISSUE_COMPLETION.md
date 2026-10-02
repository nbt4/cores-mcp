# MCP completion checklist — issues #4 and #5

This checklist records implemented business workflows and remaining acceptance
work. The parent issues stay open until every required row passes integration
and production verification. Optional event streams are separate follow-up work.

| Area | Available | Remaining |
| --- | --- | --- |
| Warehouse products | Atomic master resolution/create, metadata update, archive/restore, full typed relation create/update/archive/restore and redacted history | URL/bulk imports |
| Warehouse manufacturers/brands | Resolve, create, update with diff/version, archive/restore, retained redacted per-entity audit | — |
| Warehouse categories | Three hierarchy levels: resolve/create/update/archive/restore, exact record/dependency protection, atomic audit/replay, redacted history; authorized dependency-checked removal | — |
| Warehouse locations | Create/update/archive/restore, audit | — |
| Warehouse packages | Atomic lines/create/update/archive/restore, audit | — |
| Warehouse devices | Create/update/archive/restore, audit, own last-update revert, atomic bulk creation | — |
| Warehouse cases | Create/update/archive/restore, models search, audit | Template/content workflow tools |
| Warehouse maintenance/defects | Full recurring plans and manual work/defect create/update/transition/complete/cancel/reopen/archive/restore; atomic schedule/condition/legacy effects, events, redacted audits and explicit cost scope | — |
| Warehouse inventory | Full guided count/create/update/lines/review/correction/approve/cancel/archive/restore, explicit approve scope, precise context/line/event/start-stock protection, atomic physical adjustments/movements/audits/replay and redacted history | — |
| Warehouse tasks | Full create/partial update/start/complete/cancel/reopen/archive/restore, exact task/reference versions, atomic events/audits/durable replay and redacted history | — |
| Rental customers/venues | Full business fields, fuzzy resolve/minimal reads, partial create/update/archive/restore, signed owner action rights, active job guards, redacted audit and atomic durable replay | — |
| Rental jobs | Complete fields, shared position-based totals, live editor/device/reference/context locks, archive/restore, redacted audit, atomic native history/audit/durable replay, device assignment | — |
| Rental requirements | Create, quantity update | Complete fields, lifecycle, full atomic audit/replay |
| Rental staffing/external equipment | Read context | Create/update/lifecycle and personnel assignment |
| Procurement suppliers/products/offers | Create/update, deactivation/reactivation | Named lifecycle aliases, complete audit discovery |
| Procurement categories | Create/update with full parameter schema | Archive/restore |
| Procurement requisitions | Draft create/update, submit, separated decisions | Archive/restore, return-for-revision verification |
| Procurement orders | Draft create/update, separated transitions, partial receipt/devices/putaway | Archive/restore, end-to-end receipt discrepancy/overdelivery verification |
| Procurement product mapping | Audited versioned link | Supplier-link lifecycle verification |
| Planner plans/tasks | Create, membership-protected reads | Complete update/archive/restore, durable atomic audit/replay |
| Planner buckets/labels/dependencies/sprints/goals | Read context | Schema discovery, guided create/update/archive/restore |
| Business documents | Knowledge references only | Scoped metadata/content, quotation/invoice generation/send, attachments, labels |
| Permissions | Read-only configuration; service/action scopes; real user, target-Core rights | Documents, financial fields outside maintenance, and remaining named workflow scopes |
| Write protection | Confirmation, dry-run, bounded requests, rate limits | Verify every legacy writer has target atomic audit/replay and precise versions |
| Undo | Device, Rental customer/venue own last-field-update revert, redacted history and existing restores | Defined remaining field revert paths and per-entity history |

## Case workflow contract

Cases use named `warehouse.cases.prepare_create/create`,
`prepare_update/update`, `prepare_archive/archive`, `prepare_restore/restore`,
`audit_history` and `warehouse.case_models.search`. Preparation delegates to the
owning API and performs no business mutation. Exact versions include metadata,
contents, nesting and templates, including writes from the existing UI/scanner.

Warehouse admin plus create/update/archive scope are required. A final draft and
`confirm_change` authorize execution; lifecycle also requires
`ARCHIVE|RESTORE WAREHOUSE CASE <ID>`. Confirmed writes require an idempotency key.
Case, before/after MCP/AI audit and durable replay commit together. Archive
preserves IDs, templates, metadata and history, deactivates scan aliases and
blocks physical contents, nesting, active jobs/tasks/workflows. Restore validates
storage hierarchy/capacity, identities and template product references again.
Physical movements and case packing remain distinct processes.

## Inventory verification

WarehouseCore 5.9.103 / Cores MCP 1.5.33 adds 21 tools: 289 total
(83 reads / 103 preparations / 103 executions), Warehouse 057 / umbrella 030.
Full service tests pass with race detection and disposable PostgreSQL; Vet and
builds pass. Fresh-volume real Streamable HTTP tests verify blind reads, actual
suite user/action/approval permissions, dry-run, stale versions/start stock,
explicit missing-item review, correction, packed-case/device/quantity effects,
final-audit rollback, same-key retry, restart replay, history and lifecycle.
Production publication is tracked in the umbrella deployment docs. Parent issues
remain open until every remaining area above is complete.

## Category lifecycle verification

WarehouseCore 5.9.104 / Cores MCP 1.5.34 adds 15 tools: 304 total
(86 reads / 109 preparations / 109 executions), Warehouse 058 / umbrella 031.
Tests cover active descendant blockers, retained product/hierarchy history,
parent-first restoration, precise record and reference versions, privileged
confirmation, all-writer archive/reference/deletion guards, final-audit rollback,
same-key retry and restart replay. Production deployment is tracked in the
umbrella docs. Parent issues remain open for the remaining areas above.

## Product relationship verification

WarehouseCore 5.9.105 / Cores MCP 1.5.35 / RentalCore 5.3.116 adds 11 tools:
315 total (89 reads / 113 preparations / 113 executions), Warehouse 059 /
umbrella 032. Full race/DB tests pass in all three services, alongside Vet,
Go/Docker builds, the Rental frontend build and suite design validation.
A fresh root-migration volume verifies actual Streamable HTTP permissions,
full fields, precise product/relation/graph versions, unchanged previews and
dry-run, mandatory cycle rejection, active ancestor job guards (including
product archival), unchanged existing requirements/stock, final-audit rollback
and same-key retry, retained lifecycle history, active discovery, parent
availability, restart replay and stable legacy link tools. Production rollout
runs Warehouse/MCP before Rental to install shared lifecycle schema first.
The parent issues remain open for the remaining rows above.

## Rental customer/venue verification

RentalCore 5.3.117 / Cores MCP 1.5.36 adds 22 tools: 337 total
(95 reads / 121 preparations / 121 executions), Rental 047 / umbrella 033.
Full race/DB tests, Vet and builds pass. Rental frontend build and suite design
validation pass using the committed suite snapshot; unrelated user Planner
edits are preserved. Fresh-volume actual Streamable HTTP tests verify every
business field, signed real-user/action delegation, admin/action permissions,
pure previews and dry-run, duplicate/role validation, partial updates/clearing,
legacy stale versions, final-audit rollback and same-key retry, active job and
all-writer archive/identity/delete guards, minimal reads/redacted histories,
archived master resolution, retained historic job references, parent lifecycle
and restart durable replay. Deploy Rental first to install shared lifecycle
schema, then MCP. The parent issues remain open for the remaining rows above.

## Rental master field revert verification

RentalCore 5.3.118 / Cores MCP 1.5.37 adds four tools: 341 total
(95 reads / 123 preparations / 123 executions). Only the latest unchanged
own MCP update can be reverted with exact audit/version/context confirmation.
Tests cover both customer and venue fields, actor checks, intervening legacy
writes, repeated undo rejection, dry-run/scopes, final-audit rollback with
same-key retry, redacted history, durable restart replay and replay of a
receipt created by the previous published owner/MCP versions. Explicitly
distinct same-named archived venues restore with reviewed duplicate consent;
creation from an archived exact match remains blocked. No schema change.
Parent issues remain open for all remaining areas above.

## Rental complete job workflow verification

RentalCore 5.3.119 / Cores MCP 1.5.38 adds five tools: 346 total
(96 reads / 125 preparations / 125 executions), Rental 048 / umbrella 034.
Full Go race/DB suites, Vet, backend builds, cached unchanged Rental frontend
build and committed-suite design check pass. A fresh umbrella database and real
Streamable HTTP verify complete fields and reference resolution, nullable
clearing, explicit financial/admin/action rights, owner rights checked on cached
replay, exact confirmation/dry-run, duplicates, legacy/child/device stale
versions, active editors, device schedule conflicts, shared position-based
recalculation and financial replay scope. Status closure preserves the physical
return process. Issued devices, active cases and open warehouse tasks block
archive; archived jobs and direct/indirect contents reject native edits/deletion.
Separate actual-MCP checks cover position-device and package-reservation version
invalidation, inactive retained package restore blocking and complete contents,
status and totals retained by restore. Audit rollback/same-key retry, concurrent
exact-version writes, redacted history and durable owner/MCP restart replay pass.
Customer/venue lifecycle regression passes against the same fresh database.
Deploy Rental first and check installed job/child triggers; deploy MCP second
and verify the full read-only catalog and exact released image IDs.
The parent issues remain open for the remaining rows above.
