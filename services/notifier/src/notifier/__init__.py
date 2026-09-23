"""Notifier service: consumes order events from NATS JetStream and logs notifications."""

import asyncio
import json
import logging
import os
import signal
import sys

import nats
from nats.aio.client import Client as NATS
from opentelemetry import trace
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter
from opentelemetry.sdk.resources import SERVICE_NAME, Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor

from notifier.gen.events.v1 import order_events_pb2

logger = logging.getLogger("notifier")

def init_tracer():
    resource = Resource(attributes={SERVICE_NAME: "notifier"})
    provider = TracerProvider(resource=resource)
    processor = BatchSpanProcessor(OTLPSpanExporter())
    provider.add_span_processor(processor)
    trace.set_tracer_provider(provider)

def configure_logging() -> None:
    handler = logging.StreamHandler(sys.stdout)
    handler.setFormatter(logging.Formatter(json.dumps({
        "time": "%(asctime)s",
        "level": "%(levelname)s",
        "service": "notifier",
        "message": "%(message)s",
    })))
    logging.basicConfig(level=logging.INFO, handlers=[handler])


async def run() -> None:
    configure_logging()
    init_tracer()
    nats_url = os.getenv("NATS_URL", "nats://localhost:4222")
    nc = NATS()

    logger.info("connecting to NATS at %s", nats_url)
    try:
        await nc.connect(nats_url)
    except Exception as e:  # noqa: BLE001
        logger.error("failed to connect to NATS: %s", e)
        return

    js = nc.jetstream()

    import sqlite3
    db_path = os.getenv("DB_PATH", "events.db")
    conn = sqlite3.connect(db_path, isolation_level=None)
    conn.execute("CREATE TABLE IF NOT EXISTS seen_events (id TEXT PRIMARY KEY)")


    async def process_msg(msg: nats.aio.client.Msg) -> None:
        subject = msg.subject
        try:
            if subject == "orders.confirmed":
                event = order_events_pb2.OrderConfirmed()
                event.ParseFromString(msg.data)
                event_id = event.event_id
                try:
                    conn.execute("INSERT INTO seen_events (id) VALUES (?)", (event_id,))
                except sqlite3.IntegrityError:
                    logger.info("duplicate event %s, skipping", event_id)
                    await msg.ack()
                    return
                logger.info(
                    "notification: order %s confirmed for customer %s (total: %d %s, correlation: %s)",
                    event.order_id, event.customer_id, event.total_cents, event.currency, event.correlation_id,
                )
            elif subject == "orders.cancelled":
                event = order_events_pb2.OrderCancelled()
                event.ParseFromString(msg.data)
                event_id = event.event_id
                try:
                    conn.execute("INSERT INTO seen_events (id) VALUES (?)", (event_id,))
                except sqlite3.IntegrityError:
                    logger.info("duplicate event %s, skipping", event_id)
                    await msg.ack()
                    return
                logger.info(
                    "notification: order %s cancelled — %s (correlation: %s)",
                    event.order_id, event.reason, event.correlation_id,
                )
            else:
                logger.warning("unknown subject: %s", subject)

            await msg.ack()
        except Exception as e:  # noqa: BLE001
            logger.error("failed to process message on %s: %s", subject, e)
            await msg.nak()

    logger.info("subscribing to orders.confirmed and orders.cancelled")
    try:
        await js.subscribe(
            "orders.confirmed",
            cb=process_msg,
            durable="notifier-confirmed",
            manual_ack=True,
        )
        await js.subscribe(
            "orders.cancelled",
            cb=process_msg,
            durable="notifier-cancelled",
            manual_ack=True,
        )
    except Exception as e:  # noqa: BLE001
        logger.error("failed to subscribe: %s", e)
        await nc.close()
        return

    shutdown_event = asyncio.Event()

    def on_signal() -> None:
        shutdown_event.set()

    loop = asyncio.get_running_loop()
    for sig in (signal.SIGINT, signal.SIGTERM):
        try:
            loop.add_signal_handler(sig, on_signal)
        except NotImplementedError:
            pass

    logger.info("notifier running")
    await shutdown_event.wait()

    logger.info("shutting down")
    await nc.close()


def main() -> None:
    asyncio.run(run())


if __name__ == "__main__":
    main()
