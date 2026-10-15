#!/usr/bin/env python3
"""Build the synthetic CLAUDE_CONFIG_DIR the README screenshots are taken from.

Usage: mkfixture.py <root>
  Creates <root>/config/projects/... (point CLAUDE_CONFIG_DIR at <root>/config)
  and the matching project directory <root>/work/webapp, so running ficha from
  there auto-detects the project.

Everything here is fake: no real prompts, paths, or session data. The screenshots
are public, so keep it that way.

The random data is seeded, but timestamps are relative to now: ficha shows how
long ago the last message arrived and a rolling rate, and both read wrong against
a transcript from the past. Wall-clock times in the screenshots therefore change
on every regeneration.
"""
import argparse, json, os, random, re, uuid
from datetime import datetime, timedelta, timezone

ap = argparse.ArgumentParser(description="Build the synthetic fixture to run ficha against.")
ap.add_argument("root", help="directory to write config/ and work/ under")
args = ap.parse_args()
# argparse takes "-1" as a path on its own, and any flag after "--". A
# directory named like one is a typo far more often than a choice, and
# "./-name" still makes it.
if args.root.startswith("-"):
    ap.error("root %r looks like a flag; write ./%s for a directory of that name" % (args.root, args.root))
# An empty root is an unset variable in quotes far more often than a choice,
# and abspath would turn it into the working directory.
if args.root == "":
    ap.error("root is empty; write . for the current directory")

root = os.path.abspath(args.root)
projects = os.path.join(root, "config", "projects")
work = os.path.join(root, "work")
rng = random.Random(42)
NOW = datetime.now(timezone.utc).replace(microsecond=0)


def enc(p):
    return re.sub(r"[^a-zA-Z0-9-]", "-", p)


def ts(dt):
    return dt.strftime("%Y-%m-%dT%H:%M:%S.000Z")


def assistant(dt, model, inp, out, c5, c1h, read, sid, cwd):
    mid = "msg_" + uuid.UUID(int=rng.getrandbits(128)).hex[:24]
    return {
        "type": "assistant", "timestamp": ts(dt), "sessionId": sid, "cwd": cwd,
        "requestId": "req_" + mid[4:], "uuid": str(uuid.UUID(int=rng.getrandbits(128))),
        "message": {"id": mid, "model": model, "role": "assistant",
                    "content": [{"type": "text", "text": "ok"}],
                    "usage": {"input_tokens": inp, "output_tokens": out,
                              "cache_creation_input_tokens": c5 + c1h,
                              "cache_read_input_tokens": read,
                              "cache_creation": {"ephemeral_5m_input_tokens": c5,
                                                 "ephemeral_1h_input_tokens": c1h}}},
    }


def user(dt, sid, cwd):
    return {"type": "user", "timestamp": ts(dt), "sessionId": sid, "cwd": cwd,
            "message": {"role": "user", "content": "synthetic prompt"}}


def convo(end, n, models, sid, cwd, ctx_cap=190_000, growth=6000):
    """n assistant messages (plus the user turns between them), the last at end."""
    gaps = [rng.randint(5, 90) for _ in range(n)]
    dt, ctx, lines = end - timedelta(seconds=sum(gaps)), 8000, []
    for i in range(n):
        dt += timedelta(seconds=gaps[i])
        if i % 4 == 0:
            lines.append(user(dt - timedelta(seconds=2), sid, cwd))
        model = models[i % len(models)]
        ctx = min(ctx + rng.randint(500, growth), ctx_cap)
        c5 = rng.choice([0, 0, 0, rng.randint(500, 9000)])
        c1h = rng.choice([0, 0, 0, 0, rng.randint(1000, 20000)])
        read = max(0, ctx - c5 - c1h - 50)
        lines.append(assistant(dt, model, rng.randint(3, 400), rng.randint(40, 4000), c5, c1h, read, sid, cwd))
    return lines


def write_jsonl(path, lines):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as f:
        for line in lines:
            f.write(json.dumps(line) + "\n")
    # ficha picks the session by mtime, so it has to agree with the transcript.
    # Claude Code stamps a line before writing it, so the mtime trails the
    # newest timestamp slightly rather than matching it.
    t = datetime.strptime(lines[-1]["timestamp"], "%Y-%m-%dT%H:%M:%S.000Z").replace(tzinfo=timezone.utc).timestamp() + 0.3
    os.utime(path, (t, t))


wd = os.path.join(work, "webapp")
os.makedirs(wd, exist_ok=True)
wp = os.path.join(projects, enc(wd))
big = str(uuid.UUID(int=rng.getrandbits(128)))


def ago(**kw):
    return NOW - timedelta(**kw)


# The main conversation, ending moments before the capture so it reads as live.
# Its context grows to about 70% of Opus's 1M window, short of the ~967K
# compaction mark, so the gauge shows both the fill and the mark ahead of it.
# The growth rate gets there without hitting the cap, which would flatline
# the cache-read column.
write_jsonl(os.path.join(wp, big + ".jsonl"),
            convo(ago(seconds=12), 240, ["claude-opus-5-5"], big, wd, ctx_cap=1_000_000, growth=5000))


def write_meta(path, agent_type, description=None):
    """Write the agent-<id>.meta.json Claude Code keeps beside an agent's transcript."""
    meta = {"agentType": agent_type}
    if description:
        meta["description"] = description
    meta["spawnDepth"] = 1
    with open(path[:-len(".jsonl")] + ".meta.json", "w") as fh:
        json.dump(meta, fh)


# Plain subagents: three from earlier in the session, and one still working, so
# the bottom of breakdown interleaves agent rows with the main conversation.
# The third has no meta file, as an agent from an older Claude Code would.
for end, n, model, meta in [(ago(hours=2, minutes=20), 25, "claude-sonnet-5-5", ("Explore", "Find the auth middleware")),
                            (ago(hours=1, minutes=45), 32, "claude-haiku-4-5-20251001", ("general-purpose", "Summarize the failing tests")),
                            (ago(hours=1, minutes=5), 14, "claude-sonnet-5-5", None),
                            (ago(seconds=40), 22, "claude-sonnet-5-5", ("general-purpose", "Move sessions to Redis"))]:
    path = os.path.join(wp, big, "subagents", "agent-a%016x.jsonl" % rng.getrandbits(64))
    write_jsonl(path, convo(end, n, [model], big, wd))
    if meta:
        write_meta(path, *meta)

# One finished workflow run and one still running. Each agent is (end, message
# count, growth); a count of None draws a short run. The running run's agents
# are long and grow fast through their 1M windows. The first ends near 92%,
# inside the warning band before the ~967K compaction mark and nearer it than
# the main session, so watch names it under the gauge and in the footer. The
# second ends near 69%, short of the band, so its reading stays dim.
# Each agent's last field is its task; the last review agent's meta file has
# none, so its row falls back to the agent type.
runs = [("wf_7f3a9c", "review-changes", "completed", [(ago(hours=1, minutes=30), None, 6000, "Review the parser changes"),
                                                       (ago(hours=1, minutes=25), None, 6000, "Check error handling in the API layer"),
                                                       (ago(hours=1, minutes=18), None, 6000, None)]),
        ("wf_91bd02", "audit-codebase", "running", [(ago(seconds=25), 128, 11_800, "Audit billing for rounding"),
                                                    (ago(minutes=1, seconds=10), 109, 11_400, "Trace data-layer queries")])]
for run, name, status, agents in runs:
    rd = os.path.join(wp, big, "subagents", "workflows", run)
    for end, n, growth, task in agents:
        path = os.path.join(rd, "agent-a%016x.jsonl" % rng.getrandbits(64))
        write_jsonl(path, convo(end, n or rng.randint(8, 22), [rng.choice(["claude-fable-5-1", "claude-opus-5-5"])], big, wd, ctx_cap=1_000_000, growth=growth))
        write_meta(path, "general-purpose", task)
    os.makedirs(os.path.join(wp, big, "workflows"), exist_ok=True)
    with open(os.path.join(wp, big, "workflows", run + ".json"), "w") as fh:
        json.dump({"runId": run, "workflowName": name, "status": status}, fh)
