# Instructions for coding agents

## "Move on to the next step of the plan"

When the owner asks to move on to the next step of the plan (or the next pull request), do this, in order:

1. **Read the plan.** Read `PLAN.md` from the top: the reading instructions, §5 Conventions, the pull request map in §7, and the steps of the next pull request. The next pull request is the first one in the map that is not merged to `main` yet; check `git log` and the open pull requests to find it. If the previous pull request is not merged, stop and say so.
2. **Do the work.** Follow the plan step by step: Red, Green, Refactor, with the commit convention of §5.7. Run `make verify` after every step; it must end with `VERIFY OK`. Push after every step.
3. **Create the pull request** when the last step of the group is done, titled and described as §5.7 says. Do not put session URLs in commits, the pull request or the code.
4. **Make every build green.** Watch the CI jobs of the pull request. When one fails, read its log, fix the cause, push, and watch again. Never skip or disable a test, and never lower a threshold, to get green. Keep going until every required check is green.
5. **Stop and wait for the owner.** Do not merge. Report the pull request link, the CI result and anything that deviates from the plan, and wait for the owner to review and merge it. Start the next pull request only when asked again.
