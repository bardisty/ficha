#!/usr/bin/env python3
"""Build a large synthetic history for timing reports that read many sessions.

Usage: mkhistory.py <root> [sessions]
  Creates <root>/config/projects/ with six projects and some 300 sessions by
  default, spread over the last 60 days, roughly 400 MB in all. Point
  CLAUDE_CONFIG_DIR at <root>/config and time `ficha global`, with and
  without --since.

Every file's mtime lands just after its newest line, as it would on a real
disk. --since skips a file last written before the window, so a file whose
mtime predated its own lines would drop messages that belong in the report.
A few sessions carry agents, a corrupt line, or a fork of an older session,
so dedup and the skip counters have work to do.

Like mkfixture.py's, the data is fake and seeded, and the timestamps are
relative to now.
"""
import json, os, random, sys, uuid
from datetime import datetime, timedelta, timezone

root = os.path.abspath(sys.argv[1])
n_sessions = int(sys.argv[2]) if len(sys.argv) > 2 else 300
projects = os.path.join(root, "config", "projects")
rng = random.Random(7)
NOW = datetime.now(timezone.utc).replace(microsecond=0)
PAD = "x" * 2500  # tool output, so files weigh what real ones do per message
MODELS = ["claude-opus-5-5", "claude-sonnet-5-5", "claude-fable-5-1", "claude-haiku-4-5-20251001"]


def ts(dt):
    return dt.strftime("%Y-%m-%dT%H:%M:%S.000Z")


def assistant(dt, model, sid):
    mid = "msg_" + uuid.UUID(int=rng.getrandbits(128)).hex[:24]
    c5 = rng.choice([0, 0, rng.randint(500, 9000)])
    return {"type": "assistant", "timestamp": ts(dt), "sessionId": sid, "requestId": "req_" + mid[4:],
            "message": {"id": mid, "model": model, "role": "assistant", "content": [{"type": "text", "text": "ok"}],
                        "usage": {"input_tokens": rng.randint(3, 400), "output_tokens": rng.randint(40, 4000),
                                  "cache_creation_input_tokens": c5, "cache_read_input_tokens": rng.randint(0, 200000),
                                  "cache_creation": {"ephemeral_5m_input_tokens": c5, "ephemeral_1h_input_tokens": 0}}}}


def user(dt, sid):
    return {"type": "user", "timestamp": ts(dt), "sessionId": sid,
            "message": {"role": "user", "content": [{"type": "tool_result", "content": PAD}]}}


def convo(end, n, model, sid):
    gaps = [rng.randint(5, 60) for _ in range(n)]
    dt, lines = end - timedelta(seconds=sum(gaps)), []
    for g in gaps:
        dt += timedelta(seconds=g)
        lines.append(user(dt - timedelta(seconds=1), sid))
        lines.append(assistant(dt, model, sid))
    return lines


def write(path, lines, corrupt=False):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as f:
        for i, line in enumerate(lines):
            f.write(json.dumps(line) + "\n")
            if corrupt and i == len(lines) // 2:
                f.write('{"type":"assistant","timestamp":\n')
    newest = max(datetime.strptime(l["timestamp"], "%Y-%m-%dT%H:%M:%S.000Z") for l in lines)
    t = newest.replace(tzinfo=timezone.utc).timestamp() + 0.3
    os.utime(path, (t, t))


project_dirs = [os.path.join(projects, "-srv-proj%d" % i) for i in range(6)]
written = []
for s in range(n_sessions):
    pdir = rng.choice(project_dirs)
    sid = str(uuid.UUID(int=rng.getrandbits(128)))
    # Most sessions spread over 60 days; a handful end today.
    end = NOW - (timedelta(minutes=rng.randint(1, 180)) if s % 40 == 0 else timedelta(seconds=rng.randint(0, 60 * 86400)))
    model = rng.choice(MODELS)
    lines = convo(end, rng.randint(100, 700), model, sid)
    if written and s % 50 == 25:
        # A fork: an older session's lines, timestamps and all, then new ones.
        opdir, olines = rng.choice(written)
        pdir, lines = opdir, olines + convo(end, rng.randint(20, 80), model, sid)
        if end < datetime.strptime(olines[-1]["timestamp"], "%Y-%m-%dT%H:%M:%S.000Z").replace(tzinfo=timezone.utc):
            continue
    for a in range(rng.choice([0, 0, 0, 1, 2, 3])):
        aend = end - timedelta(seconds=rng.randint(60, 1800))
        write(os.path.join(pdir, sid, "subagents", "agent-a%016x.jsonl" % rng.getrandbits(64)),
              convo(aend, rng.randint(10, 60), rng.choice(MODELS), sid), corrupt=(a == 1))
    write(os.path.join(pdir, sid + ".jsonl"), lines, corrupt=(s % 17 == 0))
    written.append((pdir, lines))
