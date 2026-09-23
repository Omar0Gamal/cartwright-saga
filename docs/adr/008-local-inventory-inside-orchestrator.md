# Local inventory inside the orchestrator

We needed a step in the saga that could fail in order to demonstrate compensation logic.

Rather than building an entirely separate service, we put the inventory reservation logic directly inside the Orchestrator's local database transaction. While this is not entirely representative of a real-world isolated inventory service, it keeps the project scope reasonably contained to three services while still demonstrating multi-step resource coordination.
