# OAuth consent design contract

Use the canonical suite theme and existing consent primitives. Financial labels
are ordinary translated business text with no service-specific style or palette.
Warehouse lists prices, equipment revenue and maintenance costs; Rental lists
job prices, discounts, revenue, external rental costs and recalculation;
Procurement lists offer prices, requisition values and order totals. Render only
requested services. Long translated labels wrap at narrow breakpoints. Consent labels combine the
existing suite switcher label with the canonical auth text primitive, so their
secondary text color remains accessible under the actual strict Content Security
Policy. Do not use inline styles, which the production header blocks.

Access defaults to read-only; requested financial access independently defaults
to denied. Choosing financial access never selects writing or grants unrequested
services. Readonly server configuration still allows requested financial consent.
Refreshing credentials preserves the granted scopes; current active user/native
permissions remain authoritative. Static machine identities cannot grant finance.

Verify DE/EN, light/dark, 390/480/640/768/1024/1280/1536px, no horizontal overflow,
visible keyboard focus and select operation, associated help text, AA contrast,
readonly variants, no requested-finance variant and invalid/rejected consent.
Preserve normal server-rendered loading/navigation and error states.

The embedded canonical SVG uses presentation attributes (`fill="currentColor"`)
instead of inline `style` attributes, which the production CSP blocks. Path
geometry remains unchanged; the same asset stays visible in both themes.
