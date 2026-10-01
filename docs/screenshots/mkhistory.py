#!/usr/bin/env python3
"""Build a large synthetic history for timing reports that read many sessions.

Usage: mkhistory.py <root> [sessions] [--skew]
  Creates <root>/config/projects/ with six projects and some 300 sessions by
  default, spread over the last 60 days, roughly 400 MB in all. Point
  CLAUDE_CONFIG_DIR at <root>/config and time `ficha global`, with and
  without --since.

  The default history is even: six projects of one size, and no line over
  3 KB. Real ones are lopsided, and most of their bytes sit in a few very long
  lines. --skew builds that shape, in about 800 MB: 40 projects with half the
  sessions in the first, three long sessions there, the longest about a tenth
  of the history, and one tool result in 60 running from 64 KB to 2 MB. Time
  with it when a change touches how lines are read or how the work is spread
  over threads.

Every file's mtime lands just after its newest line, as it would on a real
disk. --since skips a file last written before the window, so a file whose
mtime predated its own lines would drop messages that belong in the report.
A few sessions carry agents, a corrupt line, or a fork of an older session,
so dedup and the skip counters have work to do.

Like mkfixture.py's, the data is fake and seeded, and the timestamps are
relative to now.
"""
import argparse, json, os, random, uuid
from datetime import datetime, timedelta, timezone

ap = argparse.ArgumentParser(description="Build a large synthetic history for timing ficha.")
ap.add_argument("root", help="directory to write config/projects/ under")
ap.add_argument("sessions", nargs="?", type=int, default=300, help="how many sessions to write (default 300). --skew adds three long ones")
ap.add_argument("--skew", action="store_true",
                help="shape it like a real history: 40 projects with half the sessions in one, and a few very long lines")
args = ap.parse_args()

root = os.path.abspath(args.root)
n_sessions = args.sessions
projects = os.path.join(root, "config", "projects")
rng = random.Random(7)
NOW = datetime.now(timezone.utc).replace(microsecond=0)
PAD = "x" * 2500  # tool output, so files weigh what real ones do per message
# Turns in a session. A turn weighs about 3 KB without --skew and 9 KB with
# it, so the skewed sessions are half as long to keep the history under 1 GB.
TURNS = (50, 350) if args.skew else (100, 700)
MODELS = ["claude-opus-5-5", "claude-sonnet-5-5", "claude-fable-5-1", "claude-haiku-4-5-20251001"]


def ts(dt):
    return dt.strftime("%Y-%m-%dT%H:%M:%S.000Z")


class Pad:
    """A tool result of its own length, held as that length and spelled out
    only when its line is written, so the sessions kept for forking stay small
    in memory."""

    def __init__(self, n):
        self.n = n


def tool_output():
    if not args.skew:
        return PAD
    r = rng.random()
    if r < 0.95:
        return Pad(rng.randint(200, 3000))
    if r < 0.984:
        return Pad(rng.randint(5000, 64000))
    if r < 0.999:
        return Pad(rng.randint(64000, 600000))
    return Pad(rng.randint(1000000, 2000000))


def cwd_of(pdir):
    # The directory name is the path with every "/" written as "-".
    return os.path.basename(pdir).replace("-", "/")


def assistant(dt, model, sid, cwd):
    mid = "msg_" + uuid.UUID(int=rng.getrandbits(128)).hex[:24]
    c5 = rng.choice([0, 0, rng.randint(500, 9000)])
    return {"type": "assistant", "timestamp": ts(dt), "sessionId": sid, "requestId": "req_" + mid[4:],
            "message": {"id": mid, "model": model, "role": "assistant", "content": [{"type": "text", "text": "ok"}],
                        "usage": {"input_tokens": rng.randint(3, 400), "output_tokens": rng.randint(40, 4000),
                                  "cache_creation_input_tokens": c5, "cache_read_input_tokens": rng.randint(0, 200000),
                                  "cache_creation": {"ephemeral_5m_input_tokens": c5, "ephemeral_1h_input_tokens": 0}}},
            "cwd": cwd}


def user(dt, sid, cwd):
    return {"type": "user", "timestamp": ts(dt), "sessionId": sid,
            "message": {"role": "user", "content": [{"type": "tool_result", "content": tool_output()}]},
            "cwd": cwd}


def convo(end, n, model, sid, cwd):
    gaps = [rng.randint(5, 60) for _ in range(n)]
    dt, lines = end - timedelta(seconds=sum(gaps)), []
    for g in gaps:
        dt += timedelta(seconds=g)
        lines.append(user(dt - timedelta(seconds=1), sid, cwd))
        lines.append(assistant(dt, model, sid, cwd))
    return lines


def write(path, lines, corrupt=False):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as f:
        for i, line in enumerate(lines):
            f.write(json.dumps(line, default=lambda pad: "x" * pad.n) + "\n")
            if corrupt and i == len(lines) // 2:
                f.write('{"type":"assistant","timestamp":\n')
    newest = max(datetime.strptime(l["timestamp"], "%Y-%m-%dT%H:%M:%S.000Z") for l in lines)
    t = newest.replace(tzinfo=timezone.utc).timestamp() + 0.3
    os.utime(path, (t, t))


project_dirs = [os.path.join(projects, "-srv-proj%d" % i) for i in range(40 if args.skew else 6)]
written, others = [], 0
for s in range(n_sessions):
    if args.skew:
        # Half to the first project. The rest take turns, so none is left empty.
        if rng.random() < 0.5:
            pdir = project_dirs[0]
        else:
            pdir, others = project_dirs[1 + others % (len(project_dirs) - 1)], others + 1
    else:
        pdir = rng.choice(project_dirs)
    sid = str(uuid.UUID(int=rng.getrandbits(128)))
    # Most sessions spread over 60 days; a handful end today.
    end = NOW - (timedelta(minutes=rng.randint(1, 180)) if s % 40 == 0 else timedelta(seconds=rng.randint(0, 60 * 86400)))
    model = rng.choice(MODELS)
    lines = convo(end, rng.randint(*TURNS), model, sid, cwd_of(pdir))
    if written and s % 50 == 25:
        # A fork: an older session's lines, timestamps and all, then new ones.
        # A fork can't end before the session it copies. Without --skew the
        # parent is any session written so far, and a fork of a later one is
        # dropped. --skew has fewer chances, so it picks among the earlier ones.
        parents = [w for w in written if w[2] <= end] if args.skew else written
        if not parents:
            continue
        opdir, olines, oend = rng.choice(parents)
        pdir, lines = opdir, olines + convo(end, rng.randint(20, 80), model, sid, cwd_of(opdir))
        if end < oend:
            continue
    for a in range(rng.choice([0, 0, 0, 1, 2, 3])):
        aend = end - timedelta(seconds=rng.randint(60, 1800))
        write(os.path.join(pdir, sid, "subagents", "agent-a%016x.jsonl" % rng.getrandbits(64)),
              convo(aend, rng.randint(10, 60), rng.choice(MODELS), sid, cwd_of(pdir)), corrupt=(a == 1))
    write(os.path.join(pdir, sid + ".jsonl"), lines, corrupt=(s % 17 == 0))
    written.append((pdir, lines, end))

if args.skew:
    # The long sessions. They are left out of the forks: a copy of the longest
    # would move a tenth of the history into a second file.
    for turns in (9000, 3000, 1500):
        sid = str(uuid.UUID(int=rng.getrandbits(128)))
        end = NOW - timedelta(seconds=rng.randint(0, 60 * 86400))
        write(os.path.join(project_dirs[0], sid + ".jsonl"),
              convo(end, turns, rng.choice(MODELS), sid, cwd_of(project_dirs[0])))
