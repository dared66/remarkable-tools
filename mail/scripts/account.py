#!/usr/bin/env python3
"""Connect Gmail through a local browser and retain credentials in macOS Keychain."""
import argparse
import base64
import hashlib
from http.server import BaseHTTPRequestHandler, HTTPServer
import json
from pathlib import Path
import secrets
import subprocess
import sys
import time
from urllib.error import HTTPError, URLError
from urllib.parse import parse_qs, urlencode, urlsplit
from urllib.request import Request, urlopen
import webbrowser

ROOT = Path(__file__).resolve().parents[2]
SCOPE = "https://www.googleapis.com/auth/gmail.modify"
SERVICE = "remarkable-mail"
TOKEN_URL = "https://oauth2.googleapis.com/token"


class SetupError(Exception):
    pass


def keychain():
    # Always use this app's explicit macOS Keychain adapter. An unrelated
    # skill_credentials module may select a different credential backend.
    try:
        from credential_store import CredentialError, load_record, save_record
        return CredentialError, load_record, save_record
    except ImportError:
        raise SetupError("Install mail/requirements-setup.txt in a virtual environment first.") from None


def google_json(url, *, form=None, token=None):
    data = urlencode(form).encode() if form is not None else None
    headers = {"Content-Type":"application/x-www-form-urlencoded"} if form is not None else {}
    if token:
        headers["Authorization"] = "Bearer " + token
    try:
        with urlopen(Request(url, data=data, headers=headers), timeout=30) as response:
            return json.loads(response.read(1024 * 1024))
    except (HTTPError, URLError, ValueError, OSError):
        raise SetupError("Google could not complete sign-in. Check your connection and the OAuth client configuration.") from None


def connect(client_path, account, open_browser=True):
    if not sys.stdin.isatty():
        raise SetupError("Run connect in a local Terminal so Google sign-in stays private.")
    _, _, save_record = keychain()
    try:
        client = json.loads(Path(client_path).expanduser().read_text())["installed"]
        client_id, client_secret = client["client_id"], client["client_secret"]
        if not client_id or not client_secret:
            raise ValueError()
    except (OSError, ValueError, KeyError, TypeError):
        raise SetupError("Choose a Google OAuth Desktop app client JSON file.") from None
    state = secrets.token_urlsafe(32)
    verifier = secrets.token_urlsafe(64)
    challenge = base64.urlsafe_b64encode(hashlib.sha256(verifier.encode()).digest()).decode().rstrip("=")
    result = {}

    class Callback(BaseHTTPRequestHandler):
        def log_message(self, *args):
            pass  # The URL contains a one-use authorization code.

        def do_GET(self):
            parsed = urlsplit(self.path)
            values = parse_qs(parsed.query)
            if parsed.path != "/oauth/callback":
                self.send_error(404)
                return
            if not secrets.compare_digest(values.get("state", [""])[0], state):
                self.send_error(400, "Sign-in did not match this setup session")
                return
            if values.get("error"):
                result["error"] = True
            elif values.get("code"):
                result["code"] = values["code"][0]
            else:
                self.send_error(400)
                return
            self.send_response(200)
            self.send_header("Content-Type", "text/plain; charset=utf-8")
            self.send_header("Cache-Control", "no-store")
            self.send_header("Referrer-Policy", "no-referrer")
            self.end_headers()
            self.wfile.write(b"You can close this tab and return to Terminal.")

    with HTTPServer(("127.0.0.1", 0), Callback) as server:
        server.timeout = 1
        redirect = f"http://127.0.0.1:{server.server_port}/oauth/callback"
        authorize = "https://accounts.google.com/o/oauth2/v2/auth?" + urlencode({
            "client_id":client_id, "redirect_uri":redirect, "response_type":"code",
            "scope":SCOPE, "access_type":"offline", "prompt":"consent", "state":state,
            "code_challenge":challenge, "code_challenge_method":"S256",
        })
        print("Select the Gmail account for your tablet in Google's consent screen.", flush=True)
        if not open_browser or not webbrowser.open(authorize):
            print("Open this Google sign-in link in your browser:\n" + authorize)
        deadline = time.monotonic() + 300
        while not result and time.monotonic() < deadline:
            server.handle_request()
    if not result.get("code"):
        raise SetupError("Google sign-in was cancelled or timed out. Nothing was changed in Keychain.")
    token = google_json(TOKEN_URL, form={"client_id":client_id, "client_secret":client_secret,
        "code":result["code"], "code_verifier":verifier, "redirect_uri":redirect, "grant_type":"authorization_code"})
    if not token.get("refresh_token") or not token.get("access_token"):
        raise SetupError("Google did not grant offline access. Reconnect and approve the requested Gmail access.")
    if SCOPE not in token.get("scope", "").split():
        raise SetupError("Gmail access was not granted. Nothing was changed in Keychain.")
    profile = google_json("https://gmail.googleapis.com/gmail/v1/users/me/profile", token=token["access_token"])
    email = profile.get("emailAddress")
    if not isinstance(email, str) or "@" not in email:
        raise SetupError("Google did not return a Gmail account address.")
    save_record(SERVICE, account, {"client_id":client_id, "client_secret":client_secret,
        "refresh_token":token["refresh_token"], "access_token":token["access_token"],
        "expiry":str(int(time.time()) + int(token.get("expires_in", 3600))), "email":email})
    print("Gmail connected and saved in macOS Keychain. No mail was sent or modified.")


def install_credentials(account):
    _, load_record, _ = keychain()
    config = ROOT / ".state/ssh_config"
    if not config.is_file():
        raise SetupError("The existing tablet SSH configuration is missing.")
    # Validate the Keychain record before starting a remote write, so a missing
    # or damaged record cannot replace an existing connection with empty input.
    record = load_record(SERVICE, account)
    try:
        credentials = {key: record[key] for key in
            ("client_id", "client_secret", "refresh_token", "access_token", "email")}
        if not all(isinstance(value, str) and value for value in credentials.values()):
            raise ValueError()
        credentials["expiry"] = int(record["expiry"])
        if credentials["expiry"] <= 0:
            raise ValueError()
        wire = json.dumps(credentials).encode()
    except (KeyError, TypeError, ValueError):
        raise SetupError("Saved account access is incomplete. Connect Gmail again before transferring it.") from None
    # Credentials go only through the private SSH pipe, never argv,
    # environment, tool output, or a local file. Require the known device key.
    command = "umask 077; mkdir -p /home/root/.remarkmail && chmod 700 /home/root/.remarkmail && " \
        "remarkmail_tmp=$(mktemp /home/root/.remarkmail/.credentials-XXXXXX) && " \
        "trap 'rm -f \"$remarkmail_tmp\"' EXIT && " \
        "cat > \"$remarkmail_tmp\" && chmod 600 \"$remarkmail_tmp\" && " \
        "mv \"$remarkmail_tmp\" /home/root/.remarkmail/credentials.json"
    process = subprocess.Popen(["/usr/bin/ssh", "-o", "StrictHostKeyChecking=yes", "-o", "BatchMode=yes",
        "-o", "ForwardAgent=no", "-F", str(config), "remarkable-usb", command], stdin=subprocess.PIPE, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    try:
        process.communicate(wire, timeout=35)
        if process.returncode:
            raise SetupError("Could not install account access. Check the tablet connection and retry.")
    finally:
        if process.poll() is None:
            process.kill()
            process.wait()
    print("Account access installed in a private file on the tablet. Open reMail and check connection.")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    sign_in = sub.add_parser("connect", help="Sign in through the Mac browser and save tokens in Keychain")
    sign_in.add_argument("--client", required=True, help="Downloaded Google OAuth Desktop client JSON")
    sign_in.add_argument("--account", default="default")
    sign_in.add_argument("--no-browser", action="store_true", help="Print the sign-in URL instead of launching the default browser")
    status = sub.add_parser("status", help="Report configured status without exposing credentials")
    status.add_argument("--account", default="default")
    install = sub.add_parser("install", help="Transfer saved account access over the existing tablet SSH connection")
    install.add_argument("--account", default="default")
    args = parser.parse_args()
    try:
        credential_error, load_record, _ = keychain()
        if args.command == "connect":
            connect(args.client, args.account, open_browser=not args.no_browser)
        elif args.command == "install":
            install_credentials(args.account)
        else:
            load_record(SERVICE, args.account)
            print('{"configured":true,"login_verified":false}')
        return 0
    except (SetupError, KeyboardInterrupt) as error:
        print(str(error) if isinstance(error, SetupError) else "Account setup cancelled.", file=sys.stderr)
        return 1
    except Exception:
        print("Account setup failed. Check Keychain access and configuration. No credential details were exposed.", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
