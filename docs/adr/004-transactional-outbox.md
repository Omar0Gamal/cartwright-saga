# Transactional outbox

The dual-write problem: if you update the database and then publish a message, what happens when the process dies between the two? You either have a committed state change with no event (downstream is out of sync) or a published event with no committed state (the event is a lie).

The outbox pattern sidesteps this by writing the event to an `outbox` table in the same transaction as the state change. A background goroutine polls the table and publishes to JetStream, marking rows as published only after a broker ack.

This introduces a small delay between commit and publish (the polling interval), but it guarantees the event is published if and only if the state change committed.
