"""macOS-only Keychain adapter. No plaintext or environment credential backend."""
import contextlib
import io
import json
import logging
import sys


class CredentialError(Exception):
    pass


@contextlib.contextmanager
def quiet_backend():
    previous = logging.root.manager.disable
    logging.disable(logging.CRITICAL)
    try:
        with contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
            yield
    finally:
        logging.disable(previous)


def backend():
    if sys.platform != 'darwin':
        raise CredentialError('Account setup currently requires macOS Keychain.')
    from keyring.backends.macOS import Keyring
    return Keyring()


def save_record(service, account, record):
    try:
        if not record or not all(isinstance(k, str) and isinstance(v, str) for k, v in record.items()):
            raise ValueError()
        with quiet_backend():
            backend().set_password('local.skills.' + service, account, json.dumps(record))
    except Exception:
        raise CredentialError('Could not save account access in macOS Keychain.') from None


def load_record(service, account):
    try:
        with quiet_backend():
            raw = backend().get_password('local.skills.' + service, account)
        record = json.loads(raw)
        if not record or not isinstance(record, dict) or not all(isinstance(k, str) and isinstance(v, str) for k, v in record.items()):
            raise ValueError()
        return record
    except Exception:
        raise CredentialError('Account access is missing or macOS Keychain is unavailable.') from None
