"""Exercise sign-in, callback protection, and secret handling with fake services."""
import base64
import contextlib
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import types
import unittest
from unittest.mock import patch
from urllib.parse import parse_qs, urlencode, urlsplit

spec = importlib.util.spec_from_file_location("mail_account", Path(__file__).resolve().parents[1] / "scripts/account.py")
account = importlib.util.module_from_spec(spec)
spec.loader.exec_module(account)


class AccountTests(unittest.TestCase):
    def test_connect_uses_pkce_validates_state_and_saves_only_in_keychain(self):
        observed, saved, errors = {}, [], []

        class Server:
            server_port = 34567
            timeout = 1

            def __init__(self, address, handler):
                self.handler = handler
                self.calls = 0
                self.assert_address = address == ("127.0.0.1", 0)

            def __enter__(self):
                return self

            def __exit__(self, *args):
                pass

            def handle_request(self):
                self.calls += 1
                state = observed["authorize"]["state"][0] if self.calls == 2 else "wrong-state"
                fake = types.SimpleNamespace(path="/oauth/callback?" + urlencode({"state":state, "code":"fake-code"}),
                    send_error=lambda *args:errors.append(args), send_response=lambda *args:None,
                    send_header=lambda *args:None, end_headers=lambda:None, wfile=io.BytesIO())
                self.handler.do_GET(fake)

        def browser(url):
            observed["authorize"] = parse_qs(urlsplit(url).query)
            return True

        def google(url, *, form=None, token=None):
            if form is not None:
                observed["exchange"] = form
                return {"access_token":"private-access", "refresh_token":"private-refresh", "scope":account.SCOPE, "expires_in":3600}
            self.assertEqual(token, "private-access")
            return {"emailAddress":"test@example.com"}

        with tempfile.TemporaryDirectory() as directory:
            client = Path(directory) / "client.json"
            client.write_text(json.dumps({"installed":{"client_id":"fake-client", "client_secret":"private-client"}}))
            output = io.StringIO()
            with patch.object(account, "keychain", return_value=(Exception, None, lambda *args:saved.append(args))), \
                 patch.object(account.sys, "stdin", types.SimpleNamespace(isatty=lambda:True)), \
                 patch.object(account, "HTTPServer", Server), patch.object(account.webbrowser, "open", browser), \
                 patch.object(account, "google_json", google), contextlib.redirect_stdout(output):
                account.connect(client, "default")
            self.assertEqual([p.name for p in Path(directory).iterdir()], ["client.json"])
        self.assertEqual(errors[0][0], 400)
        auth, exchange = observed["authorize"], observed["exchange"]
        digest = base64.urlsafe_b64encode(hashlib.sha256(exchange["code_verifier"].encode()).digest()).decode().rstrip("=")
        self.assertEqual(auth["code_challenge"], [digest])
        self.assertEqual(auth["scope"], [account.SCOPE])
        self.assertEqual(exchange["redirect_uri"], auth["redirect_uri"][0])
        self.assertEqual(saved[0][0:2], ("remarkable-mail", "default"))
        self.assertEqual(saved[0][2]["refresh_token"], "private-refresh")
        self.assertNotIn("private-", output.getvalue())

    def test_noninteractive_connect_is_refused_before_keychain(self):
        with patch.object(account.sys, "stdin", types.SimpleNamespace(isatty=lambda:False)), patch.object(account, "keychain") as store:
            with self.assertRaises(account.SetupError):
                account.connect("unused", "default")
            store.assert_not_called()

    def test_google_errors_do_not_expose_request_secrets(self):
        from urllib.error import URLError
        with patch.object(account, "urlopen", side_effect=URLError("private-refresh-token")):
            with self.assertRaises(account.SetupError) as result:
                account.google_json(account.TOKEN_URL, form={"refresh_token":"private-refresh-token"})
        self.assertNotIn("private-", str(result.exception))


if __name__ == "__main__":
    unittest.main()
