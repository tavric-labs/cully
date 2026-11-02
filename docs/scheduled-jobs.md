# Scheduled jobs

Cully's daemon can start a command you choose once a day, the same way it already starts advisor workers and the self-evaluation loop. Cully adds no `launchd`, `cron` or `systemd` entry for this.

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
| `command` | The program and its arguments, as a list. It is not a shell line; a leading `~/` in an item means your home directory. | required |
| `after` | Local 24-hour time. The job is due once that time has passed. | required |
| `min_hours` | Least time between two successful runs. | 20 |
| `timeout_minutes` | The job is stopped, with its child processes, after this long. | 240 |
| `max_attempts` | Attempts per scheduled time before Cully waits for the next one. | 2 |
| `retry_minutes` | Wait after a failed attempt before the next one. | 60 |

## When a job runs

- The daemon checks every five minutes. A job runs when its latest `after` time has passed, it has not succeeded since, and `min_hours` has elapsed since its last success.
- A job you just added waits for its first `after` time; it never runs the moment you save the file.
- A night missed because the computer slept, or because no Cully session was running, is made up at the next check after the daemon starts. A powered-off computer or a day without a Cully session runs nothing.
- One run at a time per job. A failed run is retried after `retry_minutes`, up to `max_attempts` for that scheduled time.
- The command starts detached at low priority with `CULLY_JOB=<name>` in its environment. Its output goes to `~/.cully/jobs/<name>.log`, trimmed at 1 MiB; state is in `~/.cully/jobs/state.json`.
- If the daemon restarts while a job is running, the job keeps running. The next daemon marks it failed if its process has gone.

## Commands

| Command | What it does |
|---|---|
| `cully jobs` or `cully jobs list` | Shows each job, its last result and when it is next due. |
| `cully jobs run NAME` | Runs a job now, in the foreground, and prints its output. It counts as a success for spacing but not against the attempt limit. |

Set `CULLY_JOBS_DISABLE=1` to stop the daemon from starting jobs.

## Safety

- Jobs come only from `~/.cully/jobs.json`. Cully and the agents it supervises never create or edit it, and `cully apply` does not touch it.
- Cully refuses the file if group or others can write it, or if another user owns it, and rejects unknown fields.
- A job is an exact program and arguments with no shell, so nothing in the file is interpreted as a command line.
- A job runs with your permissions. Treat the file as you would your shell profile.
