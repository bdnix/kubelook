# Local demo

A demo of Kubelook on your own machine, to see how it looks and try it: the console, PostgreSQL, Vault, a kind cluster
with a few workloads, and a stand-in for GitHub sign-in. It runs with [devbox](https://github.com/pilat/devbox)
on top of Docker Compose; the manifest is [`docker-compose.yaml`](../docker-compose.yaml) at the root of this repository.

**Only for your own machine.** Every port listens on `127.0.0.1` only. Don't open it to others: the sign-in stand-in lets
anyone in, Vault runs in dev mode with the root token `root`, and the encryption key in the manifest is public.

## What you need

- Docker (Docker Desktop, OrbStack, or Docker Engine on Linux) with about 4 GB of memory free.
- devbox: `brew install pilat/devbox/devbox`, or see its [installation guide](https://github.com/pilat/devbox).
- Free ports `8080` (the console), `8200` (Vault) and `8300` (the sign-in stand-in).

## Start it

```bash
devbox init https://github.com/bdnix/kubelook.git --name kubelook-demo
devbox --name kubelook-demo up
```

The first `up` asks for your password with `sudo`: devbox adds `127.0.0.1 kubelook-signin` to `/etc/hosts`, so your browser
reaches the sign-in stand-in by the same name the console uses for it. The first `up` also builds the images and creates the
kind cluster, which takes a few minutes. Later runs take seconds.

When the toolbox has finished, its log shows a Vault token:

```bash
devbox --name kubelook-demo logs toolbox
```

## Try it

1. Open <http://localhost:8080> and click **Sign in with GitHub**. The stand-in signs you in at once as
   `alice@demo.example`, with no password.
2. Add a Vault token: the one from the toolbox log, or a new one with `devbox --name kubelook-demo run token`. The cluster
   `demo` appears in the rail; open it with `devops-role` (cluster-admin) or `viewer-role` (read only).
3. Look around the namespace `shop`: a web Deployment with a Service and a PodDisruptionBudget, `orders` (JSON logs with
   info, warning and error lines), `worker` (a shell and a file `/data/hello.txt` for the file browser), a CronJob that runs
   every five minutes, a ConfigMap and a Secret. metrics-server is installed, so CPU and memory show too.

The demo users:

| User | Vault login | What the token may do |
| --- | --- | --- |
| alice | `alice` / `alice` | Open `demo` as `devops-role` or `viewer-role` |
| bob | `bob` / `bob` | Open `demo` as `viewer-role` only |

To get a token with the Vault CLI on your machine instead: `vault login -address=http://127.0.0.1:8200 -method=userpass
username=bob password=bob`.

To sign in to the console as bob, sign out first, then run `devbox --name kubelook-demo run signin-as bob` and sign in again.

To try the kubeconfig path instead of Vault, save the cluster's kubeconfig and upload it in the console:

```bash
devbox --name kubelook-demo run kubeconfig > demo-kubeconfig.yaml
```

## Scenarios

| Command | What it does |
| --- | --- |
| `devbox --name kubelook-demo run token [alice\|bob]` | Prints a Vault token of a demo user |
| `devbox --name kubelook-demo run kubeconfig [--host]` | Prints the cluster's kubeconfig: for uploading to the console, or with `--host` for `kubectl` on your machine |
| `devbox --name kubelook-demo run signin-as alice\|bob` | Chooses who the next sign-in signs in as |
| `devbox --name kubelook-demo run kubectl …` | Runs `kubectl` against the demo cluster, for example `run kubectl get pods -A` |
| `devbox --name kubelook-demo run vault …` | Runs the Vault CLI with the root token |
| `devbox --name kubelook-demo run destroy-cluster` | Deletes the kind cluster |

## How it fits together

| Service | What it is |
| --- | --- |
| `kubelook` | The console, built from this repository's `Dockerfile`, on PostgreSQL, migrating its schema at start. Configuration: [`hack/demo/kubelook.yaml`](../hack/demo/kubelook.yaml) |
| `postgres` | PostgreSQL 18, its data in the volume `kubelook-demo_postgres` |
| `vault` | Vault in dev mode: in memory, root token `root` |
| `kubelook-signin` | The identity stand-in of the integration tests (`test/integration/fakeidp`): it plays GitHub and approves every sign-in as the account the toolbox announced |
| `toolbox` | Docker CLI, kind, kubectl and Vault ([`hack/demo/toolbox.sh`](../hack/demo/toolbox.sh)). It creates the kind cluster `kubelook-demo` through the Docker socket, applies [`hack/demo/manifests.yaml`](../hack/demo/manifests.yaml), sets up the Vault mount `kubernetes/demo` with its roles, policies and userpass users, and then checks every 15 seconds: when Vault or the stand-in was restarted and forgot its state, it sets them up again |

The kind cluster is not a Compose service. The toolbox creates it with the host's Docker and connects its node to the
network `kubelook-demo`, so the console and Vault reach the API server at `https://kubelook-demo-control-plane:6443`.

## Stop and remove it

```bash
devbox --name kubelook-demo down                 # stops everything; the kind cluster keeps running
devbox --name kubelook-demo up                   # starts it again
```

To remove it for good, delete the kind cluster while the demo still runs, then remove the project:

```bash
devbox --name kubelook-demo run destroy-cluster
devbox --name kubelook-demo destroy
```

If the demo no longer runs, `docker rm -f kubelook-demo-control-plane` deletes the cluster's node instead.

## When something goes wrong

- **The sign-in ends on a page that cannot be reached at `kubelook-signin`.** The line `127.0.0.1 kubelook-signin` is
  missing from `/etc/hosts`; run `devbox --name kubelook-demo up` again, and enter your password when asked.
- **The console shows no cluster after you add a token.** The toolbox may not have finished yet; check
  `devbox --name kubelook-demo logs toolbox`.
- **A port is taken.** Stop whatever listens on `8080`, `8200` or `8300`.
