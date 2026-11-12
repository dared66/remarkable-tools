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
    def test_uses_app_keychain_adapter_even_if_unrelated_module_is_installed(self):
        scripts = str(Path(account.__file__).parent)
        unrelated = types.SimpleNamespace(CredentialError=Exception,
            load_record=lambda *args: self.fail("Unrelated storage backend used"),
            save_record=lambda *args: self.fail("Unrelated storage backend used"))
        with patch.object(account.sys, "path", [scripts] + account.sys.path), \
             patch.dict(account.sys.modules, {"skill_credentials": unrelated}):
            _, load, save = account.keychain()
        self.assertEqual(load.__module__, "credential_store")
        self.assertEqual(save.__module__, "credential_store")

    def test_invalid_keychain_record_does_not_start_remote_write(self):
        for record in ({}, {"client_id": "fake-client"}):
            with self.subTest(record=record), \
                 patch.object(account.Path, "is_file", return_value=True), \
                 patch.object(account, "keychain", return_value=(Exception, lambda *args: record, None)), \
                 patch.object(account.subprocess, "Popen") as ssh:
                with self.assertRaises(account.SetupError):
                    account.install_credentials("default")
                ssh.assert_not_called()

    def test_keychain_failure_does_not_start_remote_write(self):
        def missing(*args):
            raise account.SetupError("Account access missing")
        with patch.object(account.Path, "is_file", return_value=True), \
             patch.object(account, "keychain", return_value=(Exception, missing, None)), \
             patch.object(account.subprocess, "Popen") as ssh:
            with self.assertRaises(account.SetupError):
                account.install_credentials("default")
            ssh.assert_not_called()

    def test_account_transfer_uses_private_pipe_and_known_device_key(self):
        record = {"client_id": "fake-client", "client_secret": "fake-config",
            "access_token": "fake-access", "refresh_token": "fake-refresh",
            "email": "test@example.com", "expiry": "1900000000"}
        process = unittest.mock.Mock(returncode=0)
        process.poll.return_value = 0
        output = io.StringIO()
        with patch.object(account.Path, "is_file", return_value=True), \
             patch.object(account, "keychain", return_value=(Exception, lambda *args: record, None)), \
             patch.object(account.subprocess, "Popen", return_value=process) as ssh, \
             contextlib.redirect_stdout(output):
            account.install_credentials("default")
        command = ssh.call_args.args[0]
        self.assertIn("StrictHostKeyChecking=yes", command)
        self.assertIn("BatchMode=yes", command)
        self.assertIn("ForwardAgent=no", command)
        self.assertEqual(ssh.call_args.kwargs["stdin"], account.subprocess.PIPE)
        self.assertEqual(ssh.call_args.kwargs["stdout"], account.subprocess.DEVNULL)
        self.assertEqual(ssh.call_args.kwargs["stderr"], account.subprocess.DEVNULL)
        encoded = process.communicate.call_args.args[0]
        self.assertEqual(json.loads(encoded)["refresh_token"], record["refresh_token"])
        for key in ("refresh_token", "access_token", "client_secret", "email"):
            self.assertNotIn(record[key], " ".join(command) + output.getvalue())

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
