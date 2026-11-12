# Security and privacy

Report vulnerabilities through [GitHub's private reporting form](https://github.com/dared66/remarkable-tools/security/advisories/new).
Never put tokens, client JSON, SSH configuration, screenshots of real mail, or
mailbox exports in public issues. Use synthetic messages to report UI bugs.
Fixes target the current main branch and latest release. Successful scans do
not guarantee that experimental software has no defects.

## Data handling

- OAuth uses the system browser, PKCE, random session state, and a temporary
  callback bound to `127.0.0.1`. The Mac uses this app's explicit Keychain adapter,
  with no plaintext or environment credential-store fallback.
- Account transfer requires a known SSH host key and key authentication, with
  agent forwarding disabled. Tokens use SSH's private input pipe, never its
  command line or a temporary Mac token file.
- Tablet token files use mode `0600`, private directories use `0700`, and cached
  mail and drafts are separated by account. **reMail does not encrypt tablet
  files.** Root access or physical storage access may expose them. Other tablet
  extensions running as root share this trust boundary.
- Messages render as plain text. Scripts, remote images, tracking pixels, and
  attachments are not downloaded or executed. Mailbox data goes directly to
  Google, not to a developer backend, analytics service, or AI.
- Sending requires confirmation. Uncertain delivery is recorded rather than
  retried automatically. Revoking Google access does not erase local copies.

See the [privacy policy](https://krupapc.dev/apps/remail/privacy/) and
[removal instructions](mail/INSTALL.md#updates-removal-and-troubleshooting).
The shared Gmail-access review is separate from source publication; the current
release uses the installer's own Desktop OAuth client.

## Publication checks

Never publish `.state/`, Keychain exports, account credentials, access or refresh
tokens, private keys, SSH configuration, real mailbox data, or private review
recordings. `.gitignore` is a convenience, not a security boundary. Review every
staged diff and the complete release file list.

A future shared sign-in may distribute only verified **Desktop/native** app
configuration after checking its type and contents. Native configuration cannot
be confidential; see [Google's guidance](https://developers.google.com/identity/protocols/oauth2/native-app).
That never permits user tokens or web/server credentials. Secret-scan exceptions
must be narrow and reviewed; do not disable token rules to make a release pass.

GitHub secret scanning, push protection, dependency alerts, automatic security
fix proposals, and private reporting are enabled. The security workflow checks
complete history and dependency vulnerabilities on changes and daily.
Dependabot proposes weekly updates; proposals still need review and checks.

```sh
gitleaks git --redact --no-banner --log-opts=--all .
gitleaks dir --redact --no-banner .
(cd mail && govulncheck ./... && go test ./... && go vet ./...)
pip-audit -r mail/requirements-setup.txt -r mail/requirements-build.txt
python3 -m unittest discover -s mail/tests
```

Also check the Linux ARMv7 build, extracted package and embedded frontend.
Source scans alone do not establish what a released binary contains. Public
screenshots must use synthetic messages. Re-run checks before launch.
