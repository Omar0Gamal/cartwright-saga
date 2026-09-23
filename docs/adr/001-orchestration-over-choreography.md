# Orchestration over choreography

Choreography (services react to each others' events) is appealing for loose coupling, but it makes the checkout flow nearly impossible to debug when it goes wrong. With choreography, the "what step are we on?" question requires correlating events across three services' logs.

Orchestration puts the entire workflow in one place. The trade-off is a single point of failure (the orchestrator), but that's a known quantity you can defend with lease-based recovery and crash-safe state — which is the whole point of this project.
