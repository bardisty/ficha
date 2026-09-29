#!/usr/bin/env python3
"""Simulate a running Claude Code session by appending to a synthetic fixture.

Point it at a session in a fixture from mkfixture.py (make fixture) while
watch or breakdown runs on it. Everything it writes is fake: the text of
every message is "ok", and the ids and token counts are random. It refuses
a target outside a fixture's config/projects directory.

Usage:
  live.py <session.jsonl> [--interval 2] [--count 20] [--model claude-opus-5-5]
      append assistant messages to an existing session
  live.py <session.jsonl> --agent [--interval 2] [--count 20]
      create a new subagent transcript under <session>/subagents/ and append to it
  live.py <project-dir> --new-session [--interval 2] [--count 20]
      start a brand-new session file in that project dir (tests auto-follow)
  live.py <session.jsonl> --burst 50
      append 50 messages at once (tests large jumps / scroll anchoring)
"""
import argparse, json, os, random, time, uuid
from datetime import datetime, timezone

ap = argparse.ArgumentParser()
ap.add_argument("target")
ap.add_argument("--interval", type=float, default=2.0)
ap.add_argument("--count", type=int, default=20)
ap.add_argument("--model", default="claude-opus-5-5")
ap.add_argument("--agent", action="store_true")
ap.add_argument("--new-session", action="store_true")
ap.add_argument("--burst", type=int, default=0)
a = ap.parse_args()
rng = random.Random()

if a.new_session:
    path = os.path.join(a.target, str(uuid.uuid4()) + ".jsonl")
elif a.agent:
    base = a.target[:-len(".jsonl")]
    path = os.path.join(base, "subagents", "agent-a%016x.jsonl" % rng.getrandbits(64))
else:
    path = a.target
# A fixture's transcripts live under <root>/config/projects. The default
# ~/.claude keeps them under projects/ alone, so this refuses real sessions
# unless someone named their config directory "config".
if "/config/projects/" not in os.path.abspath(path).replace(os.sep, "/"):
    raise SystemExit("live.py: %s is not inside a fixture's config/projects" % a.target)
os.makedirs(os.path.dirname(os.path.abspath(path)), exist_ok=True)
sid = os.path.basename(a.target)[:-6] if a.target.endswith(".jsonl") else os.path.basename(path)[:-6]


def last_context(path):
    """The context the file's newest assistant message sent, so appends carry
    on from it instead of dropping the context gauge back to a few percent."""
    ctx = 20000
    try:
        with open(path) as f:
            for raw in f:
                try:
                    msg = json.loads(raw).get("message")
                except ValueError:
                    continue
                u = msg.get("usage") if isinstance(msg, dict) else None
                if u:
                    ctx = u.get("input_tokens", 0) + u.get("cache_creation_input_tokens", 0) + u.get("cache_read_input_tokens", 0)
    except FileNotFoundError:
        pass
    return ctx


ctx = last_context(path)


def line():
    global ctx
    ctx += rng.randint(500, 8000)
    now = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%S.%f")[:-3] + "Z"
    mid = "msg_" + uuid.uuid4().hex[:24]
    c5 = rng.choice([0, 0, rng.randint(500, 5000)])
    return json.dumps({"type": "assistant", "timestamp": now, "sessionId": sid, "requestId": "req_" + mid[4:],
                       "message": {"id": mid, "model": a.model, "role": "assistant", "content": [{"type": "text", "text": "ok"}],
                                   "usage": {"input_tokens": rng.randint(3, 300), "output_tokens": rng.randint(50, 3000),
                                             "cache_creation_input_tokens": c5, "cache_read_input_tokens": ctx,
                                             "cache_creation": {"ephemeral_5m_input_tokens": c5, "ephemeral_1h_input_tokens": 0}}}})


print("appending to", path, flush=True)
if a.burst:
    with open(path, "a") as f:
        for _ in range(a.burst):
            f.write(line() + "\n")
    raise SystemExit
for i in range(a.count):
    with open(path, "a") as f:
        f.write(line() + "\n")
    print("appended", i + 1, flush=True)
    time.sleep(a.interval)
