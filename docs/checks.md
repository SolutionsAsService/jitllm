# Which checks a change needs

`scripts/checks/map.json` maps each subsystem, by path prefix, to the checks
it needs, the models and devices those need, and the evidence file AGENTS.md's
routing table sends a reader to first. It is drawn from the gates AGENTS.md
names; the rules themselves stay in AGENTS.md.

    go run ./scripts/checks                     # the working tree's changes
    go run ./scripts/checks HEAD~3..HEAD        # a diff range
    go run ./scripts/checks engine/model/x.go   # paths
    go run ./scripts/checks -run HEAD~1..HEAD   # run them, each under scripts/cap
    go run ./scripts/checks -json ...           # the plan or the results as JSON

A path under several prefixes gets every entry's checks. Each go test runs as
`./scripts/cap MEM -- taskset -c 0,2,4,6,8,10 go test ... -count=1` (RULE 3,
RULE 5); a check that needs models reads `JITLLM_MODELS` (`docs/testing.md`).

With `-run` each check reports one of `pass`, `fail`,
`skipped-missing-model`, `skipped-missing-device`, `skipped-other` or
`no-tests` (the pattern selected nothing). A check with passes and skips
reports the skip, and the exit code is 0 only when every check passed: a skip
is not a pass (RULE 10). Missing hardware is a reason to run the check where
the hardware is, never a green line. Entries with a `manual` note name what
no automated check covers.

`scripts/checks.TestMapCoversTheTree` holds every tracked file to some entry,
every `-run` alternative to a test in its packages, and every evidence path to
a file; a new top-level directory, or a gate renamed under the map, fails it.
Measurements (`dev/bench`, `scripts/vs-llamacpp.sh`) are not in the map: they
are run serially by one session (RULE 2, RULE 3).
