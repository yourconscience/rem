#!/usr/bin/env python3
"""SessionStart context backed by AgentsView.

AgentsView indexes every harness's own transcripts (locally and from federated
remote hosts), so nothing here writes session digests. SessionStart injects a
compact list of recent sessions read from `agentsview session list --json`.
When AgentsView is missing or unreachable the hooks degrade to an empty
context instead of failing the session. No third-party Python dependency.
"""

from __future__ import annotations

import json
import os
import re
import shutil
import subprocess
import sys
import time
from datetime import datetime
from pathlib import Path
from typing import Any, TextIO

from safety import truncate_redacted

MAX_CONTEXT_CHARS = 6000
RECENT_SINCE = "7d"
RECENT_LIMIT = 12
FIRST_MESSAGE_CHARS = 200
AGENTSVIEW_TIMEOUT_SECS = 8
LAZY_SYNC_MAX_AGE_SECS = 12 * 3600
PINS_LIMIT = 10
PINS_TIMEOUT_SECS = 3
PINS_REMOTE_MAX_AGE_SECS = 3600
# Session text from these projects never enters another session's context.
PRIVATE_PROJECTS = {
    name.strip().casefold()
    for name in os.environ.get("REM_PRIVATE_PROJECTS", "personal").split(",")
    if name.strip()
}


class HookError(Exception):
    """Actionable hook failure."""


class AgentsViewUnavailable(Exception):
    """AgentsView could not answer; the caller degrades to empty context."""


def _error(message: str) -> int:
    print(f"rem hook error: {message}", file=sys.stderr)
    return 1


def parse_payload(stdin: TextIO) -> dict[str, Any]:
    try:
        payload = json.load(stdin)
    except json.JSONDecodeError as exc:
        raise HookError(f"invalid hook JSON on stdin: {exc.msg} at line {exc.lineno} column {exc.colno}") from exc
    if not isinstance(payload, dict):
        raise HookError("hook payload must be a JSON object")
    return payload


def context_json(context: str) -> str:
    return json.dumps(
        {
            "continue": True,
            "suppressOutput": True,
            "hookSpecificOutput": {
                "hookEventName": "SessionStart",
                "additionalContext": context,
            },
        },
        sort_keys=True,
        separators=(",", ":"),
    )


def agentsview_binary() -> str | None:
    override = os.environ.get("AGENTSVIEW_BIN", "").strip()
    candidates = [override] if override else [shutil.which("agentsview") or "", str(Path.home() / ".local/bin/agentsview"), "/opt/homebrew/bin/agentsview"]
    for candidate in candidates:
        if candidate and os.path.isfile(candidate) and os.access(candidate, os.X_OK):
            return candidate
    return None


def remote_hosts() -> list[str]:
    """Host names from [[remote_hosts]] in the AgentsView config; never reads other fields."""
    data_dir = Path(os.path.expanduser(os.environ.get("AGENTSVIEW_DATA_DIR", "~/.agentsview")))
    try:
        text = (data_dir / "config.toml").read_text()
    except OSError:
        return []
    hosts: list[str] = []
    in_remote = False
    for raw in text.splitlines():
        line = raw.strip()
        if line.startswith("["):
            in_remote = line.replace(" ", "") == "[[remote_hosts]]"
            continue
        if in_remote:
            match = re.match(r'host\s*=\s*"([^"]+)"', line)
            if match:
                hosts.append(match.group(1))
    return hosts


def lazy_sync_state_dir() -> Path:
    state = os.environ.get("XDG_STATE_HOME") or str(Path.home() / ".local/state")
    return Path(state) / "rem" / "agentsview-sync"


def lock_is_held(lock: Path) -> bool:
    try:
        owner = int((lock / "pid").read_text().strip())
    except (OSError, ValueError):
        # Spawner has not written the pid yet; treat a fresh lock as held.
        try:
            return time.time() - lock.stat().st_mtime < 60
        except OSError:
            return False
    try:
        os.kill(owner, 0)
    except ProcessLookupError:
        return False
    except PermissionError:
        return True
    return True


def lazy_remote_sync() -> tuple[list[str], list[str]]:
    """Start a detached `agentsview sync --host H` for each remote host whose last
    successful lazy sync is older than LAZY_SYNC_MAX_AGE_SECS. Never blocks.

    Returns (stale hosts, hosts whose sync was started now or is already running).
    """
    hosts = remote_hosts()
    if not hosts:
        return [], []
    binary = agentsview_binary()
    state_dir = lazy_sync_state_dir()
    stale: list[str] = []
    syncing: list[str] = []
    for host in hosts:
        safe = re.sub(r"[^A-Za-z0-9._-]", "_", host)
        stamp = state_dir / f"{safe}.last"
        try:
            age = time.time() - stamp.stat().st_mtime
        except OSError:
            age = None
        if age is not None and age < LAZY_SYNC_MAX_AGE_SECS:
            continue
        stale.append(host)
        if binary is None:
            continue
        lock = state_dir / f"{safe}.lock"
        try:
            state_dir.mkdir(parents=True, exist_ok=True)
            lock.mkdir()
        except FileExistsError:
            if lock_is_held(lock):
                syncing.append(host)
                continue
            (lock / "pid").unlink(missing_ok=True)
            try:
                lock.rmdir()
                lock.mkdir()
            except OSError:
                continue
        except OSError:
            continue
        # The stamp is written only on success, so a failed sync retries next time.
        script = (
            'trap \'rm -f "$1/pid"; rmdir "$1" 2>/dev/null\' EXIT; '
            'if "$4" sync --host "$5" >"$3" 2>&1; then date -u +%Y-%m-%dT%H:%M:%SZ >"$2"; fi'
        )
        try:
            proc = subprocess.Popen(
                ["/bin/sh", "-c", script, "sh", str(lock), str(stamp), str(state_dir / f"{safe}.log"), binary, host],
                stdin=subprocess.DEVNULL,
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
                start_new_session=True,
            )
        except OSError:
            lock.rmdir()
            continue
        try:
            (lock / "pid").write_text(f"{proc.pid}\n")
        except OSError:
            pass  # The sync already exited and its trap removed the lock.
        syncing.append(host)
    return stale, syncing


def staleness_note(stale: list[str], syncing: list[str]) -> str:
    if not stale:
        return ""
    hours = LAZY_SYNC_MAX_AGE_SECS // 3600
    note = f"Possibly stale: sessions from {', '.join(stale)} were last synced over {hours}h ago (or never)"
    if syncing:
        note += f"; background `agentsview sync` running for {', '.join(syncing)}"
    return truncate_redacted(note + ".", 500)


def recent_sessions() -> list[dict[str, Any]]:
    binary = agentsview_binary()
    if binary is None:
        raise AgentsViewUnavailable("agentsview binary not found")
    command = [binary, "session", "list", "--json", "--since", RECENT_SINCE, "--limit", str(RECENT_LIMIT + 1)]
    # A hook must stay fast and side-effect free: never auto-start a daemon.
    env = dict(os.environ, AGENTSVIEW_NO_DAEMON="1")
    try:
        result = subprocess.run(
            command,
            capture_output=True,
            text=True,
            timeout=AGENTSVIEW_TIMEOUT_SECS,
            stdin=subprocess.DEVNULL,
            env=env,
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise AgentsViewUnavailable(f"agentsview session list failed: {exc}") from exc
    if result.returncode != 0:
        detail = (result.stderr.strip().splitlines() or ["no stderr"])[0]
        raise AgentsViewUnavailable(f"agentsview session list exited {result.returncode}: {detail}")
    try:
        data = json.loads(result.stdout)
    except json.JSONDecodeError as exc:
        raise AgentsViewUnavailable(f"agentsview session list returned invalid JSON: {exc.msg}") from exc
    sessions = data.get("sessions") if isinstance(data, dict) else None
    if not isinstance(sessions, list):
        raise AgentsViewUnavailable("agentsview session list JSON has no sessions array")
    return [session for session in sessions if isinstance(session, dict)]


def local_time(value: Any) -> str:
    if not isinstance(value, str) or not value:
        return "unknown time"
    try:
        parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError:
        return truncate_redacted(value, 40)
    return parsed.astimezone().strftime("%Y-%m-%d %H:%M %z")


def is_private(project: Any, cwd: Any) -> bool:
    names = {str(project or ""), *Path(str(cwd or "")).parts}
    return any(name.casefold() in PRIVATE_PROJECTS for name in names)


def format_session(session: dict[str, Any]) -> str:
    machine = str(session.get("machine") or "local").removesuffix(".local")
    parts = [
        local_time(session.get("ended_at") or session.get("started_at")),
        f"{session.get('agent') or 'unknown'}@{machine}",
        str(session.get("project") or "no project"),
        f"{session.get('user_message_count', 0)} user msgs",
    ]
    outcome = session.get("outcome")
    if outcome and outcome != "unknown":
        parts.append(f"outcome={outcome}")
    parts.append(f"id={session.get('id')}")
    line = "- " + truncate_redacted(" | ".join(str(part) for part in parts), 300)
    first = session.get("first_message")
    if not is_private(session.get("project"), session.get("cwd")) and isinstance(first, str) and first.strip():
        line += "\n  first request: " + truncate_redacted(first, FIRST_MESSAGE_CHARS)
    return line


def build_context(sessions: list[dict[str, Any]], exclude_id: str | None, max_chars: int = MAX_CONTEXT_CHARS) -> str:
    header = (
        f"Recent coding-agent sessions from AgentsView (last {RECENT_SINCE}, newest first, all indexed machines). "
        "Load details with the agentsview MCP tools or `agentsview session get|messages <id>`.\n"
    )
    lines: list[str] = []
    total = len(header)
    for session in sessions:
        if exclude_id and session.get("id") == exclude_id:
            continue
        if len(lines) >= RECENT_LIMIT:
            break
        entry = format_session(session)
        if total + len(entry) + 1 > max_chars:
            break
        lines.append(entry)
        total += len(entry) + 1
    if not lines:
        return ""
    return header + "\n".join(lines)


def ccd_sessions_binary() -> str | None:
    override = os.environ.get("CCD_SESSIONS_BIN", "").strip()
    candidates = [override] if override else [shutil.which("ccd-sessions") or "", str(Path.home() / ".local/bin/ccd-sessions")]
    for candidate in candidates:
        if candidate and os.path.isfile(candidate) and os.access(candidate, os.X_OK):
            return candidate
    return None


def refresh_remote_pins_async(binary: str, host: str, cache: Path) -> None:
    """Detached `ccd-sessions list --host H` into the cache; ssh is too slow for the hook."""
    marker = cache.with_suffix(".refreshing")
    try:
        if time.time() - marker.stat().st_mtime < 60:
            return
    except OSError:
        pass
    try:
        cache.parent.mkdir(parents=True, exist_ok=True)
        marker.touch()
        subprocess.Popen(
            ["/bin/sh", "-c", '"$1" list --pinned --json --host "$2" >"$3.$$" 2>"$4" && mv "$3.$$" "$3"; rm -f "$3.$$" "$5"',
             "sh", binary, host, str(cache), str(cache.with_suffix(".log")), str(marker)],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            start_new_session=True,
        )
    except OSError:
        pass


def desktop_pins() -> list[dict[str, Any]]:
    """Pinned Claude Desktop sessions from every account: live on this Mac,
    cached for AgentsView remote hosts. Empty when ccd-sessions is absent."""
    binary = ccd_sessions_binary()
    if binary is None:
        return []
    listings: list[dict[str, Any]] = []
    try:
        result = subprocess.run([binary, "list", "--pinned", "--json"], capture_output=True, text=True,
                                timeout=PINS_TIMEOUT_SECS, stdin=subprocess.DEVNULL)
        if result.returncode == 0:
            listings.append(json.loads(result.stdout))
    except (OSError, subprocess.TimeoutExpired, ValueError) as exc:
        print(f"rem: desktop pins skipped: {exc}", file=sys.stderr)
    cache_dir = lazy_sync_state_dir().parent / "ccd-sessions"
    for host in remote_hosts():
        cache = cache_dir / (re.sub(r"[^A-Za-z0-9._-]", "_", host) + ".json")
        try:
            age = time.time() - cache.stat().st_mtime
            listings.append(json.loads(cache.read_text()))
        except (OSError, ValueError):
            age = None
        if age is None or age > PINS_REMOTE_MAX_AGE_SECS:
            refresh_remote_pins_async(binary, host, cache)
    pins: dict[tuple[str, str], dict[str, Any]] = {}
    for listing in listings:
        if not isinstance(listing, dict):
            continue
        host = str(listing.get("host") or "local").removesuffix(".local")
        for session in listing.get("sessions") or []:
            if not isinstance(session, dict):
                continue
            key = (host, str(session.get("sessionId")))
            # The same session can sit in several accounts; prefer the signed-in copy.
            if key not in pins or session.get("signedIn"):
                pins[key] = dict(session, host=host)
    return sorted(pins.values(), key=lambda s: str(s.get("lastActivityAt") or ""), reverse=True)


def format_pin(pin: dict[str, Any]) -> str:
    parts = [
        local_time(pin.get("lastActivityAt")),
        f"{pin.get('host')}",
        "(private)" if is_private(None, pin.get("cwd")) else str(pin.get("title") or "untitled"),
        str(pin.get("cwd") or "no cwd"),
        f"cli={pin.get('cliSessionId')}",
    ]
    if not pin.get("signedIn"):
        parts.append(f"other account {str(pin.get('account'))[:8]}")
    if pin.get("transcript") is False:
        parts.append("no transcript on that host")
    return "- " + truncate_redacted(" | ".join(parts), 300)


def build_pins_context(pins: list[dict[str, Any]], exclude_id: str | None, max_chars: int) -> str:
    header = (
        "Pinned Claude Desktop sessions (all accounts, this Mac and AgentsView remote hosts). "
        "Resume with `claude --resume <cli>` in that cwd on that host; "
        "`ccd-sessions adopt --id <cli>` shows an other-account one in this Desktop account after a restart.\n"
    )
    lines: list[str] = []
    total = len(header)
    for pin in pins:
        if exclude_id and pin.get("cliSessionId") == exclude_id:
            continue
        if len(lines) >= PINS_LIMIT:
            break
        entry = format_pin(pin)
        if total + len(entry) + 1 > max_chars:
            break
        lines.append(entry)
        total += len(entry) + 1
    if not lines:
        return ""
    return header + "\n".join(lines)


def session_start(stdin: TextIO = sys.stdin, stdout: TextIO = sys.stdout) -> int:
    try:
        payload = parse_payload(stdin)
    except HookError as exc:
        return _error(str(exc))
    session_id = payload.get("session_id") or payload.get("sessionId")
    exclude = str(session_id) if session_id else None
    note = staleness_note(*lazy_remote_sync())
    budget = MAX_CONTEXT_CHARS - (len(note) + 1 if note else 0)
    pins = build_pins_context(desktop_pins(), exclude, budget // 3)
    budget -= len(pins) + 1 if pins else 0
    try:
        context = build_context(recent_sessions(), exclude, budget)
    except AgentsViewUnavailable as exc:
        print(f"rem: recent sessions skipped: {exc}", file=sys.stderr)
        context = ""
    if pins:
        context = pins + ("\n" + context if context else "")
    if note:
        context = note + ("\n" + context if context else "")
    print(context_json(context), file=stdout)
    return 0


if __name__ == "__main__":
    # `basic_memory.py lazy-sync`: the same non-blocking remote refresh the
    # SessionStart hook runs, for skills such as /load. Prints the staleness note.
    if sys.argv[1:] != ["lazy-sync"]:
        raise SystemExit("usage: basic_memory.py lazy-sync")
    print(staleness_note(*lazy_remote_sync()) or "AgentsView remote hosts synced within the last 12h (or none configured).")
