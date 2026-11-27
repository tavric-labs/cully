# Scheduled jobs

Cully's daemon can start a command you choose on a schedule (once a day by default), the same way it already starts advisor workers and the self-evaluation loop. Cully adds no `launchd`, `cron` or `systemd` entry for this.

**Audience:** you want a recurring local task, such as a nightly audit script, started by the Cully binary instead of a separate scheduler. **Prerequisites:** a running Cully setup; the job's own tools must already work from your shell.

## Define a job

Create `~/.cully/jobs.json` yourself and restrict it to your account:

```json
{
  "jobs": [
    {
      "name": "nightly-audit",
      "command": ["python3", "~/nightly-audit/run.py"],
      "after": "22:30"
    }
  ]
}
```

```sh
chmod 600 ~/.cully/jobs.json
cully jobs
```

| Field | Meaning | Default |
|---|---|---|
| `name` | Lowercase letters, digits and dashes. | required |
| `command` | The program and its arguments, as a list. It is not a shell line. A leading `~/` in an item means your home directory. The program must be an absolute path, `~/path`, or a bare name found on `PATH`; `./run.sh` is rejected because it would depend on where the daemon was started. | required |
| `after` | Local 24-hour time. The job is due once that time has passed. Needed for `daily` and `weekdays`; leave it out for `hourly`, `every` and `in`. | required for daily |
| `schedule` | When it runs; see [Schedules](#schedules). | daily, at `after` |
| `max_runs` | Stop after this many scheduled runs (1 to 1000). A retry of a failed run is not a new run. | no limit |
| `min_hours` | Least time between two successful runs. | 20 |
| `timeout_minutes` | The job is stopped, with its child processes, after this long. | 240 |
| `max_attempts` | Attempts per scheduled time before Cully waits for the next one. | 2 |
| `retry_minutes` | Wait after a failed attempt before the next one. | 60 |

## Schedules

`schedule` takes exactly these forms, the same ones an agent takes, plus a one-shot delay. Anything else is refused; cron expressions are not supported.

| Form | Meaning |
|---|---|
| (omitted) or `daily` | Once a day, after the `after` time. |
| `weekdays` | The same, but never on Saturday or Sunday. |
| `hourly` | Every hour. |
| `every 15m` to `every 168h` | Every N minutes (`m`) or hours (`h`), from 15 minutes to 168 hours. |
| `in 30m` to `in 168h` | One run, that long after the job is saved (1 minute to 168 hours). Cully records the time in `run_at`. |

```json
{"name": "fetch-often", "command": ["git", "-C", "/home/me/app", "fetch", "--all"], "schedule": "every 2h", "max_runs": 12}
```

An interval is measured from the start of the last run, and the first run comes one interval after the daemon first sees the job. Pausing keeps the job and its history; resuming gives at most one catch-up run.

## When a job runs

- The daemon checks every five minutes. A job runs when its latest `after` time has passed, it has not succeeded since, and `min_hours` has elapsed since its last success.
- A job you just added waits for its first `after` time; it never runs the moment you save the file. Removing a job and adding it back starts it fresh.
- A night missed because the computer slept, or because no Cully session was running, is made up at the next check after the daemon starts. A powered-off computer or a day without a Cully session runs nothing.
- Downtime never queues runs: whatever the schedule, a daemon that was off for many periods starts one catch-up run, then continues normally.
- A tick is skipped while the job's previous run is still running; nothing is queued behind it.
- A one-shot (`in`) job stops after it succeeds. A job with `max_runs` stops once that many scheduled runs have started. Manual runs do not count toward the limit.
- One run at a time per job, across the daemon and `cully jobs run`. A failed run is retried after `retry_minutes`, up to `max_attempts` for that scheduled time.
- The command starts detached at low priority, in your home directory, with `CULLY_JOB=<name>` added to the environment of the process that started the daemon (so `PATH` and other variables come from the session that launched it; use absolute paths for anything that matters). Its output goes to `~/.cully/jobs/<name>.log`. When a run starts and the log is over 1 MiB, the old log is kept as `<name>.log.1`. State is in `~/.cully/jobs/state.json`; a state file that cannot be read is moved aside as `state.json.corrupt-<time>`, never overwritten.
- A timeout stops the job's whole process group: SIGTERM, then SIGKILL for anything still left after a short grace period.
- If the daemon restarts while a job is running, the job keeps running without a timeout. The next daemon marks it failed once its process has gone, or once it has run for twice its timeout.
- A time recorded in the future (a clock that was wrong once) is ignored rather than blocking the job.

## Run history and pausing

Each run is recorded in Cully's database (`cully.db`): when it started and ended, whether it finished, failed or timed out, and the last few lines of its output with secrets such as `password=...` and tokens redacted. The last 20 runs per job are kept, and the failure streak (failed runs in a row, newest first) is computed from them. The full output stays in `~/.cully/jobs/<name>.log`.

Pause and resume are kept in `~/.cully/jobs/state.json`, not in `jobs.json`. A paused job never starts on its own; Run now still works.

## Commands

| Command | What it does |
|---|---|
| `cully jobs` or `cully jobs list` | Shows each job, its last result and when it is next due. |
| `cully jobs run NAME` | Runs a job now, in the foreground, and prints its output; Ctrl-C stops the job too. It counts as a success for spacing but not against the attempt limit. |

## The Background jobs page

The browser page shows each job as a card: its state, schedule, the last runs as bars, the last result, and Run now, Pause or Resume, Edit and Remove.

- **Describe a job** turns a sentence into an unsaved draft. The advisor writes one `JOB|` line; Cully checks the command against the allowlist below, the folder against the folders it already knows, and the schedule against the forms above. Nothing is written until you press Approve and add, and the server checks the command again when you do. A draft that is not one fixed allowed command is refused.
- **Edit** changes only the schedule, time and run limit. The command never changes from the page; remove the job and describe it again.
- **Remove** deletes the job from `jobs.json` and its run history.
- A scheduled run never creates a job. Only a request from this page, or approving an Advisor proposal, writes `jobs.json`.

Set `CULLY_JOBS_DISABLE=1` to stop the daemon from starting jobs.

## Safety

- Jobs come only from `~/.cully/jobs.json`. Cully writes to it only when you approve a job on the browser page (an Advisor proposal or a described draft), edit a job's schedule, or remove a job: it adds or changes that one job, with the exact command and time you saw, and leaves every other job as written. `cully apply` does not touch it. A proposal must be one of a few fixed shapes: `git -C DIR fetch|status`, `make -C DIR test|lint|build|check|vet|ci|fmt-check`, `go -C DIR test|build|vet ./...`, `npm --prefix DIR test|run test|lint|build`, or `pytest -q DIR`. Anything else is refused, including shells, `git -c`, `find -exec`, `go test -exec`, interpreters with inline code, wrappers and paths to other programs. `make`, `go` and `npm` still run what that folder's own Makefile, module or `package.json` says, so read the command before approving.
- Cully refuses the file if group or others can write it, or if another user owns it, and rejects unknown fields and trailing data.
- A job is an exact program and arguments with no shell, so nothing in the file is interpreted as a command line.
- A job runs with your permissions. Treat the file as you would your shell profile: anything that runs as you, including a coding agent, can edit it, and these checks are not a defence against that. Review changes to it the way you would review changes to a login script.
