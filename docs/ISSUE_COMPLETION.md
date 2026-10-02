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
| Rental requirements | Full total/manual/source quantities, exact line/job/context versions, archive/restore, redacted audit, atomic native history/replay and retained native selections; archived demand excluded | — |
| Rental staffing/external equipment | Read context | Create/update/lifecycle and personnel assignment |
| Procurement suppliers/products/offers | Create/update, retained named archive/restore, exact record/dependency context, current owner rights, atomic audit/activity/durable replay, per-entity redacted versioned history | — |
| Procurement categories | Full parameter schema create/update, retained named archive/restore, active product/parent guards, redacted audit and atomic owner activity/replay | — |
| Procurement requisitions | Draft create/update, submit, complete context-bound distinct-administrator decisions, retained archive/restore, verified return → revise → resubmit and redacted versioned history | Legacy writer protection verification |
| Procurement orders | Draft create/update, complete context-bound status transitions, full context-bound receipt, retained archive/restore with putaway blockers and versioned history | Legacy draft protection and supplier submission verification |
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

## Rental complete material requirement verification

RentalCore 5.3.120 / WarehouseCore 5.9.106 / Cores MCP 1.5.39 adds seven tools:
353 total (99 reads / 127 preparations / 127 executions), Rental 049 /
umbrella 035. Full Go race/DB suites, Vet/builds, unchanged frontend builds and
committed suite design validation pass. An upgrade from the previously published
owners preserves old successful job receipts and original material identity and
quantities; unversioned legacy writes must be prepared again. Fresh umbrella
schema and actual Streamable HTTP verify complete total/manual/source arithmetic,
explicit zero manual quantities, current administrator/action rights on replay,
exact phrase/dry-run, duplicate/archived identity resolution, stale line/parent/
product/position/device/editor contexts, assignment floors, archive blockers,
retained lifecycle and hard-delete/all-writer guards. Active demand and Warehouse
requirement reads exclude archives. Parent/product/source validation, final-audit
rollback/same-key retry, concurrent exact-version execution, redacted line/native
job history and durable restart replay pass. Native selection tests preserve the
original line ID through soft removal/reselection and position reconciliation.
Database and actual generated/regenerated Warehouse PDF tests include extra
manual quantities beside commercial positions, accessory multiplication, archive
exclusion and retained restore.
Deploy Rental first, then Warehouse and MCP; verify exact images, installed
requirement/job guards and the complete read-only catalog. Parent issues remain
open for the remaining rows above.

## Complete fresh stack compatibility

ProcurementCore 1.0.65 preserves original SQL-migration UNIQUE constraints
through repeated GORM startup; full PostgreSQL/race tests, Vet/build and all
23 frontend tests pass. Root 036 mirrors the existing committed Planner
003/004 schema, which was previously missing from fresh umbrella installation.
MCP 1.5.40 offer unit prices and receipt percentages explicitly convert floating
quantities to PostgreSQL numeric before decimal rounding and retain NULL for
zero denominators. Integration tests exercise actual named tools and curated
offer queries with fractional and zero quantities. These compatibility fixes
retain the 353-tool catalog; remaining issue acceptance work stays open.


## Procurement retained catalog lifecycle and OAuth

Procurement 1.0.66 / MCP 1.5.41: 368 tools (102 reads / 133 preparations /
133 executions). Supplier/product/offer archive and restoration have separate
named tools and redacted audit aliases; complete original fields and dependency
versions bind exact confirmation. Open orders/requisitions block affected archives;
offer restoration requires active parents. Native 008 / umbrella 037 retains
identity, history and monotonic versions for every catalog writer. Existing native
DELETE returns 409 and cannot silently remove price/history records.

Full PostgreSQL race suites, Vet/build, actual owning API and SDK/MCP tests cover
atomic audit rollback/retry, current real-user/admin/archive rights including
cached and restarted replay, stale dependency context and concurrent single-version
execution. Actual Streamable HTTP verifies all six lifecycle actions, redacted
versioned history and retained fields/prices. Upgraded and separately fresh complete
stacks expose 368 tools with zero read-only smoke failures. OAuth PKCE tests verify
Rental/Procurement archive and explicit Rental financial scopes, deny-default
financial consent and preservation of the requested scope set. Browser checks cover
German/English, Light/Dark, 390/768/1280/1536px, keyboard focus and no horizontal
layout overflow. Committed design copies pass validation; user-owned pre-existing
Planner copies remain untouched. Deploy Procurement first, then MCP, and verify
all existing guards plus the three proc_*_guard_lifecycle_version triggers.
The parent issues remain open for the other acceptance rows above.


## Procurement category lifecycle

Procurement 1.0.67 / MCP 1.5.42: 373 tools (103 reads / 135 preparations /
135 executions). Native 009 / umbrella 038 adds retained category lifecycle and
all-writer identity/version guards. Active products block archives; active product
creation/restoration locks and checks its category. Ordinary category updates retain
state. Category/supplier resolution keeps historical IDs as restoration_required.
Complete original parameter definitions and other fields are retained. Optional
parameter units/options are correctly optional in MCP schemas; selects still require
valid options. Older category request hashes and saved successful responses preserve
their pre-active-field shape; original supplier lifecycle receipts replay unchanged.

Full PostgreSQL/race suites, Vet/build, 23 unchanged frontend tests, frontend build
and committed design validation pass. Actual Streamable HTTP against the upgraded
and fresh complete stacks verifies create, archive/restore, bound exact version and
context, current administrator/archive rights, dry-run, active product/category
all-writer blockers, retained parameter definitions, atomic final-audit rollback and
same-key retry, redacted history and restart replay. Complete read-only smoke has
373 tools with zero failures. Deploy Procurement before MCP and verify the new
category trigger alongside all previously released guards. Parent issues remain
open for the other acceptance rows.

## Complete goods receipt verification

Procurement 1.0.68 / MCP 1.5.43 retains 373 tools (103 reads / 135 preparations /
135 executions). Both receipt tools delegate directly to the closed owning API.
An exact record and complete context freeze order/line state, active mapping,
stock/location distribution, serial reservations, destination and supplier/Amazon
confirmation alongside the quantity-bound phrase. Partial/full/overdelivery,
individual/quantity/untracked/service items, bounded serials and physical precision
are explicit. Receipt/order/line, stock/devices, putaway task/event, native activity,
versioned before/after audits and durable response commit together. Legacy MCP
receipt execution only replays previously saved successes; unbooked old previews
must use the new owner context. Current owner administrator/receive rights are
checked before cached, legacy and restarted replay. Generic legacy write access
cannot grant receipt or procurement decisions/status transitions; their dedicated
receive/approve scopes must be explicitly consented.

Full PostgreSQL/race suites, Vet/build, 23 unchanged frontend tests, frontend build
and committed design validation pass. Actual Streamable HTTP against upgraded
and separately fresh complete stacks verifies all physical receipt modes, partial
and final status/unchanged prices, explicit overdelivery, quantity/line/mapping/
stock/distribution/zone/serial staleness, Amazon partial confirmation ceilings,
current scope/role revocation, dry-run, concurrent single commit, final warehouse
and order audit rollback/same-key retry, per-entity redacted versioned history and
owner/MCP restart replay. A golden receipt created by the previous published
native owner retains its saved response without creating a second stock booking.
The fresh full read-only catalog has zero failures. Deploy Procurement before MCP,
verify exact healthy images and every previously installed guard. There is no new
schema migration. The parent issues remain open for the other acceptance rows.

## Procurement workflow lifecycle and revision

Procurement 1.0.69 / MCP 1.5.44 adds ten tools: 383 total (105 reads / 139
preparations / 139 executions), native 010 / umbrella 039. Both requisitions and
orders retain original status, all fields, line IDs and receipt/history links on
archive/restore. Current owner rights and archive scope, exact microsecond version,
complete dependency fingerprint and record/context-bound phrase are required.
Requisition rights are requester/admin; orders require current administrator.
Open related orders and pending receipt putaway tasks block archival. Restore
validates original active catalog parents and original requisition. Old putaway
associations are backfilled from receipt audit records. All-writer parent/child
guards preserve identities, protect archives and advance parent versions for line
edits. Operative native/MCP lists exclude archives; historical references remain.

Full PostgreSQL/race suites, Vet/build, all 23 unchanged frontend tests, cached
frontend build and committed suite design check pass. Upgraded and separately
fresh complete stacks verify real Streamable HTTP scopes/current roles and
revocation, pure previews/dry-run, all four retained lifecycle actions, stale
native line/parent/dependency context, active restore parents, final-audit rollback
and same-key retry, concurrent single commit, redacted versioned history and
owner/MCP restart replay. Actual older supplier and native receipt golden results
replay unchanged without duplicate physical effects. An actual new receipt stores
its putaway association; archive remains blocked until the separate named Warehouse
task completion succeeds. Actual different-administrator return, requester revision
back to draft and requester resubmission clear the previous decision while retaining
audit history. Historical native requisition audit versions are now readable too.

Deploy Procurement first, then MCP; verify exact healthy released images, every
previous guard and the six workflow parent/child triggers. Legacy draft/decision/
transition owner protection, other permission/financial/document/Undo areas and all
other remaining rows still require completion before closing either parent issue.

## Complete owner approval/status verification

Procurement 1.0.70 / MCP 1.5.45 retains 383 tools. Decisions and status transitions
now delegate directly to a closed owner API without stale pre-SQL replay guards.
Current active administrator and explicitly consented approve scope are checked
before every new, cached, legacy or restarted execution. Requisition decisions
also require a different user than the immutable original requester. Complete
record/lines, current catalog/parent/receipt state, reviewed action/note/reason,
exact microsecond version and complete context bind elevated confirmation.
Native change/audit/activity/durable response commit together. Original successful
native approval/cancellation hashes and business responses remain replayable;
unbooked public legacy MCP requests require the new context-bound owner path.
Cancellation retains all stock/receipts and independent putaway obligations.
Sent records status and does not submit an external supplier order; Amazon requires
its original native submission flow. Generic write scope cannot authorize approvals.

Retained order restore now validates business fields independently of the original
received/cancelled status and preserves that status. Complete shared workflow
previews are bounded to 512 KiB per original record. Schema discovery includes
separate requisition decision/submission and order status/receipt inputs.

Full PostgreSQL/race suites, Vet/builds, all 23 unchanged frontend tests, cached
frontend build and committed design validation pass. Actual Streamable HTTP against
upgraded and separately fresh complete stacks verifies all approval/rejection/return
and sent/confirmed/cancellation actions, current scope/admin/separation checks,
pure previews/dry-run, line/catalog/action/reason staleness, atomic final-audit
rollback/same-key retry, concurrent single exact-version decision and owner/MCP
restart replay. Actual older published native approval and cancellation golden
business results replay unchanged; changed original payloads are rejected.
The public legacy decision route only replays its saved original result. Actual
cancelled and received order restore keeps original status/fields/receipt links.
Actual different-admin return, requester revision and resubmission clear the old
decision, while versioned redacted history remains. No migration is added.

Deploy Procurement first, then MCP, and verify exact healthy images, every existing
guard and the complete read-only catalog. Other acceptance rows, legacy draft
writers, supplier submission and remaining financial/document/Undo protection stay
open; neither parent issue is complete yet.
