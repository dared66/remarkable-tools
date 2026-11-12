#!/usr/bin/env python3
"""Check real Gmail reads on the tablet, reporting counts without mail content."""
import json
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
SSH = ["/usr/bin/ssh", "-T", "-F", str(ROOT / ".state/ssh_config"), "remarkable-usb",
       "/home/root/xovi/exthome/appload/RemarkMail/backend/entry --stdio"]


def batch(requests):
    wire = "".join(json.dumps(request) + "\n" for request in requests).encode()
    result = subprocess.run(SSH, input=wire, stdout=subprocess.PIPE,
                            stderr=subprocess.DEVNULL, timeout=60)
    if result.returncode:
        raise RuntimeError("The tablet mail service did not complete the check.")
    replies = [json.loads(line) for line in result.stdout.splitlines()]
    if len(replies) != len(requests):
        raise RuntimeError("The tablet returned an incomplete check response.")
    for request, reply in zip(requests, replies):
        if reply.get("id") != request["id"]:
            raise RuntimeError("The tablet returned an unexpected check response.")
        if not reply.get("ok"):
            raise RuntimeError(reply.get("error", "Gmail read check failed."))
    return [reply["result"] for reply in replies]


def main():
    try:
        status, listing = batch([
            {"id":1,"action":"status","params":{}},
            {"id":2,"action":"list","params":{"folder":"inbox"}},
        ])
        if not status.get("configured") or status.get("demo"):
            raise RuntimeError("Connect a real Gmail account before this check.")
        if listing.get("cached"):
            raise RuntimeError("The tablet returned cached mail; a live Gmail read is still unverified.")
        report = {"configured":True, "demo":False, "live_inbox_read":True,
                  "conversations_on_first_page":len(listing["threads"]),
                  "has_more":bool(listing.get("next")), "email_sent_or_modified":False}
        if listing["threads"]:
            thread, = batch([{"id":3,"action":"thread","params":{"id":listing["threads"][0]["id"]}}])
            if thread.get("cached"):
                raise RuntimeError("The conversation was cached; a live message read is still unverified.")
            report["live_conversation_read"] = True
            report["messages_in_checked_conversation"] = len(thread["messages"])
        path = ROOT / ".state/mail-live-verification.json"
        path.write_text(json.dumps(report, indent=2) + "\n")
        print(json.dumps(report, indent=2))
        return 0
    except (RuntimeError, OSError, ValueError, subprocess.TimeoutExpired) as error:
        print(str(error) if isinstance(error, RuntimeError) else "The live Gmail read check could not complete.", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
