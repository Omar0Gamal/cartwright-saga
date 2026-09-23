"""
Contract tests: can the notifier decode the events the orchestrator publishes?
De-duplication is tested here once that logic exists.
"""

from google.protobuf.timestamp_pb2 import Timestamp

from notifier.gen.events.v1 import order_events_pb2


class TestProtoContract:

    def test_order_confirmed_round_trip(self):
        original = order_events_pb2.OrderConfirmed(
            event_id="evt-1",
            order_id="ord-1",
            customer_id="cust-1",
            total_cents=4999,
            currency="USD",
            occurred_at=Timestamp(seconds=1700000000),
            correlation_id="corr-1",
        )
        raw = original.SerializeToString()

        parsed = order_events_pb2.OrderConfirmed()
        parsed.ParseFromString(raw)

        assert parsed.event_id == "evt-1"
        assert parsed.order_id == "ord-1"
        assert parsed.total_cents == 4999
        assert parsed.correlation_id == "corr-1"

    def test_order_cancelled_round_trip(self):
        original = order_events_pb2.OrderCancelled(
            event_id="evt-2",
            order_id="ord-2",
            customer_id="cust-2",
            reason="insufficient stock",
            correlation_id="corr-2",
        )
        raw = original.SerializeToString()

        parsed = order_events_pb2.OrderCancelled()
        parsed.ParseFromString(raw)

        assert parsed.reason == "insufficient stock"
        assert parsed.customer_id == "cust-2"

    def test_empty_payload_parses_without_raising(self):
        event = order_events_pb2.OrderConfirmed()
        event.ParseFromString(b"")
        assert event.event_id == ""
        assert event.total_cents == 0
