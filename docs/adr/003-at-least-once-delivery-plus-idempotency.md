# At-least-once delivery plus idempotency

Exactly-once delivery is a lie in distributed systems (or at least, it comes at a cost that isn't worth paying here). Instead: assume every message might be delivered more than once, and make every handler safe to call twice with the same input.

In practice, this means:
- The orchestrator derives a deterministic idempotency key per step, so a retry after a crash reuses the same key.
- The billing service stores each key alongside the result in the same transaction, so a duplicate call returns the stored response without mutating anything.
- The outbox publisher sets `Nats-Msg-Id` to the outbox row's ID, so JetStream deduplicates at the broker level.

The cost is bookkeeping (the `idempotency_records` table, the outbox table). The benefit is that you can retry any timeout without thinking about it.
