# 0003 — MQTT as the device protocol

Status: accepted

## Context
Gateways sit behind NAT and cellular links and go offline often. The server must send commands (upgrade, config, reboot) and receive heartbeats and metrics.

## Decision
MQTT 3.1.1 over TLS with a per-device client certificate (CN = serial number). Devices publish to `fleet/{sn}/hb|metrics|status|ack` and subscribe to `fleet/{sn}/cmd`. QoS 1 for commands and acks, QoS 0 for metrics. Desired state is kept as a retained per-device message so offline devices converge on reconnect. Broker: Mosquitto. Details go in `docs/PROTOCOL.md` (task E1).

## Consequences
- Devices only make outbound connections; no inbound ports on gateways.
- Broker is a small extra process; the server is an MQTT client, not a broker.
- Rollout state transitions come from agent reports, so timeouts per state are required in the engine.
