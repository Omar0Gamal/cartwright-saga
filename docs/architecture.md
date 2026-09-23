# Architecture

## Overview

Cartwright is three services, one database server (two databases), and a message broker:

![Architecture Diagram](architecture.svg)

## Service boundaries

Each service owns its data. The orchestrator never touches the billing database. Contracts live in `/proto` — no language owns them.

The orchestrator owns local inventory (an `inventory` table in its own database). This isn't how you'd build a real inventory service, but it gives the saga a second resource to coordinate without adding a fourth service.

## Communication patterns

| Path | Protocol | Why |
|---|---|---|
| Client → Orchestrator | HTTP/JSON | Simple public-facing API |
| Orchestrator → Billing | gRPC | Synchronous — the saga needs the result before it can proceed |
| Orchestrator → Notifier | NATS JetStream (via outbox) | Asynchronous — the notifier can be down without blocking checkout |

## Data flow

An order goes through four steps: authorize payment → reserve stock → capture payment → confirm order. If any step fails before capture, the orchestrator runs compensations in reverse. After capture (the "pivot"), the saga only rolls forward.

State transitions and outbox events are written in the same Postgres transaction, so an event is emitted if and only if the state change committed.
