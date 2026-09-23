# Deterministic idempotency keys

When the orchestrator calls billing, it needs an idempotency key. The naive approach is to generate a UUID and store it before making the call. But if the process dies after generating the key and before storing it, the key is lost, and the retry generates a new one — which means billing treats it as a new request and you get a double-charge.

Instead, the key is derived as `{order_id}-{step}-{direction}`. This is fully deterministic from data that's already durable (the order ID and the step name). A crashed orchestrator can resume mid-step and produce the exact same key without any pre-call bookkeeping.

The downside: keys are tightly coupled to the step enum. If you rename a step, outstanding in-flight keys become orphaned. In practice this doesn't matter because step names don't change at runtime.
