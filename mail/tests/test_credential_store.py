"""Check Keychain-only storage and secret-free failure paths without real credentials."""
import contextlib
import io
import sys
from pathlib import Path
import unittest
from unittest.mock import Mock, patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
import credential_store as store


class CredentialStoreTests(unittest.TestCase):
    def test_private_record_roundtrip_and_namespace(self):
        fake = Mock()
        record = {'refresh_token': 'private-token', 'email': 'test@example.com'}
        with patch.object(store, 'backend', return_value=fake):
            store.save_record('remarkable-mail', 'default', record)
            service, account, encoded = fake.set_password.call_args.args
            fake.get_password.return_value = encoded
            self.assertEqual(store.load_record('remarkable-mail', 'default'), record)
        self.assertEqual((service, account), ('local.skills.remarkable-mail', 'default'))

    def test_failure_and_backend_output_are_sanitized(self):
        def fail(*args):
            print('private-token')
            print('private-token', file=sys.stderr)
            raise RuntimeError('private-token')
        fake = Mock()
        fake.set_password.side_effect = fail
        output = io.StringIO()
        with patch.object(store, 'backend', return_value=fake), contextlib.redirect_stdout(output), contextlib.redirect_stderr(output):
            with self.assertRaises(store.CredentialError) as result:
                store.save_record('remarkable-mail', 'default', {'token': 'private-token'})
        self.assertNotIn('private-token', output.getvalue() + str(result.exception))

    def test_other_platform_refuses_file_or_environment_fallback(self):
        with patch.object(store.sys, 'platform', 'linux'):
            with self.assertRaises(store.CredentialError):
                store.backend()
