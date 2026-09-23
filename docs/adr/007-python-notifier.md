# Python notifier

To handle event consumption, we wrote the Notifier service in Python.

This decision helps validate that the protobuf event contracts and NATS JetStream setup work seamlessly across a polyglot architecture (incorporating Go, C#, and Python). While it does add a third language toolchain to the repository, proving this multi-language compatibility is worth the extra overhead.
