# Instructions for coding agents

## "Move on to the next step of the plan"

When the owner asks to move on to the next step of the plan (or the next pull request), do this, in order:

1. **Read the plan.** Read `PLAN.md` from the top: the reading instructions, §5 Conventions, the pull request map in §7, and the steps of the next pull request. The next pull request is the first one in the map that is not merged to `main` yet; check `git log` and the open pull requests to find it. If the previous pull request is not merged, stop and say so.
2. **Do the work.** Follow the plan step by step: Red, Green, Refactor, with the commit convention of §5.7. Run `make verify` after every step; it must end with `VERIFY OK`. Push after every step.
3. **Create the pull request** when the last step of the group is done, titled and described as §5.7 says. Do not put session URLs in commits, the pull request or the code.
4. **Make every build green.** Watch the CI jobs of the pull request. When one fails, read its log, fix the cause, push, and watch again. Never skip or disable a test, and never lower a threshold, to get green. Keep going until every required check is green.
5. **Stop and wait for the owner.** Do not merge. Report the pull request link, the CI result and anything that deviates from the plan, and wait for the owner to review and merge it. Start the next pull request only when asked again.

## When the whole plan is finished

The plan is finished when its last pull request (the one that ends with the §8 Definition of done) is complete. In that last pull request, as its final commit, archive the plan:

1. Create `docs/plans/` if it does not exist.
2. Move `PLAN.md` there with `git mv`, so history is kept, and name it with the UTC time of the move so the completed plans sort in time order: `docs/plans/YYYYMMDD-HHMMSS-plan.md`, for example `docs/plans/20261120-143005-plan.md` (`date -u +%Y%m%d-%H%M%S`).
3. Check that nothing else points at `PLAN.md` (`grep -rn PLAN.md .`) and fix any link to the new path.
4. Run `make verify`, push, and keep going until CI is green, as above. Then wait for the owner to review and merge.

Do this only when the whole plan is done, not after each pull request.

## Docker in the Claude Code cloud environment

The cloud container has Docker Engine installed (`docker`, `dockerd`, `containerd`) and runs as root, but the daemon is not started for you. Each new session starts a fresh container, so start the daemon once per session before any `docker` command:

```sh
# Start the daemon in the background if it is not already running, and wait until it answers.
if ! docker info >/dev/null 2>&1; then
  nohup dockerd >/tmp/dockerd.log 2>&1 &
  for i in $(seq 1 30); do docker info >/dev/null 2>&1 && break; sleep 1; done
fi
docker info --format '{{.ServerVersion}} {{.Driver}}'   # fails if the daemon did not come up
```

If `docker info` still fails, read `/tmp/dockerd.log`.

Then pull and run images as usual. Docker Hub pulls work through the environment's proxy:

```sh
docker run --rm hello-world
```

Notes:

- Before the daemon starts, `docker` commands fail with `failed to connect to the docker API at unix:///var/run/docker.sock`. That means the daemon is not running. Docker is installed.
- The daemon checked on 2026-11-19 reported Docker 29.10.0, the `overlayfs` storage driver and cgroup v1.
- Images, containers and the daemon go away when the session's container is reclaimed. Do not rely on them between sessions.
- Only starting the daemon, pulling from Docker Hub and running a container have been checked here. Port publishing, container networking, privileged containers and kind inside Docker have not. Check them before you rely on them, and update this section with what you find.
