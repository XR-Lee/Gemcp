# Finance ledger and internal credits

Gemcp Finance is an Owner-only internal scheduling ledger. It reports monthly Project capacity, active reservations, estimated Provider charges, manual credits and debits, daily activity, backend attribution, immutable ledger entries, and finance-related audit events.

It is not a payment processor. A Gemcp credit does not charge, refund, or add funds to an AutoDL account. AutoDL's console remains authoritative for actual account balance and invoicing.

## Capacity model

For one Project and monthly period, Gemcp computes:

$$
\text{available} = \text{base allocation} + \text{credits} - \text{debits} - \text{net reservations} - \text{estimated charges}
$$

The Project's `monthly_budget_milli` is the base allocation. Credits and debits are period-scoped internal adjustments. New adjustments always apply to the Project's current billing period; the period selector is a history filter and cannot backdate a new entry. Reservations are released at terminal settlement and replaced by an estimated charge when the Provider runtime has a billable lifetime. Self-hosted execution records zero-value reservations and releases.

Historical base allocation reflects the Project's current policy because Project budget changes are not versioned yet. Ledger entries and their original periods remain immutable.

## Owner operations

The console exposes **Finance / Budget and ledger** with:

- organization or Project filtering;
- a monthly period selector;
- base, credit, debit, reservation, charge, and available totals;
- daily activity and backend cost attribution;
- per-Project capacity;
- the latest 200 ledger entries;
- the latest 100 organization-wide finance-related audit events;
- an append-only credit/debit dialog.

Finance endpoints require an authenticated Owner session and CSRF protection:

```text
GET  /api/v1/finance?period=YYYY-MM&project_id=<optional-project-id>
POST /api/v1/projects/:id/budget-adjustments
```

Adjustment input uses integer milli-CNY and a Project-scoped idempotency key:

```json
{
  "direction": "credit",
  "amount_milli": 50000,
  "reason": "Approved July AutoDL test allocation",
  "idempotency_key": "budget-550e8400-e29b-41d4-a716-446655440000"
}
```

A repeated identical request returns the original entry. Reusing the key with a different direction, amount, or reason is rejected. Entries cannot be edited or deleted; corrections use a new opposite entry.

## Storage and audit

Project-level adjustments use `budget_entries.kind=adjustment` without a synthetic Experiment relation. A credit is stored as a negative ledger amount because it reduces committed capacity; the API also returns `balance_effect_milli`, where a positive value increases available capacity.

Every adjustment writes an audit event with the Owner identity, Project, period, direction, amount, and immutable Budget Entry target. Finance audit responses intentionally omit event metadata so operational details are not broadly rendered in the console.
