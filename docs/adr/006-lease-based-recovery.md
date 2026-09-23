# Lease-based recovery with SKIP LOCKED

If an Orchestrator worker crashes, any stalled saga it was processing must be picked up by another worker.

We handle this using a lease model with Postgres's `SELECT ... FOR UPDATE SKIP LOCKED`. This approach is very simple and relies on existing database features to prevent multiple replicas from picking up the same saga simultaneously. The main trade-off is that it relies on reasonably synchronized clocks across application instances.
