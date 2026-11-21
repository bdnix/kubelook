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

## Open findings

(Written after the run below.)

## Results

Each run appends a section below.

