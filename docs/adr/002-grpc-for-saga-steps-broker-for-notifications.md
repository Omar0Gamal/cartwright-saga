# gRPC for saga steps, NATS for notifications

The orchestrator needs to talk to billing and to the notifier, but the reliability requirements are completely different.

Billing calls are synchronous. The saga can't proceed until it knows whether the authorize succeeded, got declined, or timed out — each of those three outcomes leads to a different state transition. gRPC gives us typed contracts and status codes that map cleanly onto those outcomes.

Notifications are fire-and-forget from the saga's perspective. The order is already confirmed by the time we publish; if the notifier is down, JetStream holds the message until it comes back. No workflow step depends on the notification result.
