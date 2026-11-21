# Watch-hub load test

`make load` (or `hack/load.sh`, which also appends the result to this file) measures the watch hub at the scale the
spec proposes. It is a measurement, not a pass/fail test: it prints a table and exits 0 whatever the numbers are. It
uses real time and runs only on demand and in the nightly workflow, never in the `ci` workflow.

## What it does

- Starts an in-process fake Kubernetes API server (`test/load/fakeapiserver.go`, a TLS `httptest` server). It serves
  `GET /api/v1/pods` with N generated pods and, for every `watch=true` request, a stream of M `MODIFIED` events per
  second. Each event carries the time it was emitted in an annotation.
- Starts a real Kubelook server (`app.Serve`) on a loopback port with a SQLite database, and creates S users with one
  session each, the way sign-in does. Every user uploads a kubeconfig that points at the fake API server, so every
  user holds their own upstream watch (users never share one).
- Opens S WebSocket clients, each with a `watch.open` for all pods, and lets the events flow for the measured time.
- Records the live heap before and after the watches are open (after a full garbage collection), the time from the
  open request to the last snapshot frame, the time from an event's emission to its arrival at the client, and the
  refused opens.

Defaults: N = 1000 pods, S = 200 sessions, M = 50 events per second per watch, 60 s. Change them with
`LOAD_PODS`, `LOAD_SESSIONS`, `LOAD_EVENTS_PER_SECOND` and `LOAD_SECONDS`.

## Limits of the measurement

- Everything runs in one process on one machine: the heap growth is the whole process (Kubelook, the fake API server
  and the clients), not Kubelook alone, so it overstates the memory per watch.
- Latency is measured on the loopback interface. It leaves out the network between the browser and Kubelook and
  between Kubelook and the API server, and the time the browser takes to draw.
- The fake API server answers with generated pods of a fixed small shape. A real cluster's pods are larger.

## Targets of the spec

| Target (spec, "Non-functional requirements", proposed) | Measure here |
| --- | --- |
| First list render under 2 s for 1,000 pods | Time to the complete first snapshot |
| Watch event to UI update under 1 s end to end | Event emitted → received by the client |
| 200 active sessions per replica | Run with S = 200: no refused opens, no failed clients |

## Comparison with the targets (run of 2026-11-21, 16:59 UTC, commit 032f340, below)

| Target | Result | Met? |
| --- | --- | --- |
| First list render under 2 s for 1,000 pods | p50 1.64 s, p95 2.07 s, p99 2.12 s when all 200 sessions open their list in the same moment. With 20 sessions opening together the p99 was 0.23 s (a trial run, not recorded below). | Met at p50, **missed at p95 and p99 in the 200-at-once burst** |
| Watch event to UI update under 1 s end to end | p50 0.2 ms, p95 1.4 ms, p99 7.9 ms from emission to the client, over 604,827 events (the network and the browser's drawing are not included) | Met |
| 200 active sessions per replica | 200 sessions, 200 upstream watches, 50 events per second each (10,000 events per second in all), one minute: no refused opens, no failed clients | Met |

## Open findings

- **First snapshot of 1,000 pods when 200 users open a list in the same moment misses the 2 s target at p95 and p99**
  (2.07 s and 2.12 s; p50 is 1.64 s). The 200 snapshots (1,000 pods each) are produced, encoded and read by
  clients on the same machine, so the whole measurement shares its CPUs with the clients; the test cannot say how
  much of the time is Kubelook's. The spec calls the target a proposal "to be confirmed after the spike". No
  threshold was changed. Whether the target needs another look, or the hub needs work, is for the repository owner to
  decide.
- **Memory per watch** was about 2.8 MiB of live heap per user's watch of 1,000 pods, measured for the whole process,
  so it includes the fake API server and the clients and overstates Kubelook's share. A figure for Kubelook alone
  needs the Prometheus gauges of Step 6.1 (`kubelook_watch_cached_objects`) read from a separate process, or the load
  test run against a Kubelook container.

## Results

Each run appends a section below.

### 2026-11-21 16:59 UTC, commit 032f340

Parameters: 1000 pods, 200 sessions (one user and one watch each), 50 events per second per watch, 1m0s.

| Measure | Result |
| --- | --- |
| Live heap growth per watch (whole process, includes the load generator) | 2896.3 KiB |
| Time to the complete first snapshot, p50 / p95 / p99 | 1644.6 ms / 2072.4 ms / 2119.3 ms |
| Event emitted → received by the client, p50 / p95 / p99 | 0.2 ms / 1.4 ms / 7.9 ms |
| Events received (latency samples) | 604827 (604827) |
| Upstream watches opened on the fake API server | 200 |
| Refused opens | 0 |
| Clients that failed | 0 |

