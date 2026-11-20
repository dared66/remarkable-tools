# reMarkable tools

Small, independent apps for the reMarkable tablet, by Krupa Poobala-Chandran.

| Tool | What it does | Status |
| --- | --- | --- |
| [reFinder](finder/) | Browse My files, Obsidian vaults, and device files offline. | Tested on reMarkable 2; build from source |
| [reMail](mail/) | Read mail over IMAP, send via SMTP, and keep local drafts on reMarkable 2. | Experimental; downloadable ARMv7 release |

Start with the **[reMail installation guide](mail/INSTALL.md)** or the
**[latest reMail release](https://github.com/dared66/remarkable-tools/releases/tag/remail-v0.2.0-rc1)**.
The [website](https://krupapc.dev/apps/remail/) has the same setup overview,
privacy policy, and terms.

reFinder was checked on reMarkable 2, firmware 3.29.1.0: My files listing, document details, and refreshing the on-device view passed. It uses the same AppLoad and XOVI setup as reMail.

reMarkdown will join this collection later. It is not included in this release.

These are unofficial tools, not affiliated with Google or reMarkable. reMail's
first release was tested on reMarkable 2, firmware 3.29.1.0, with AppLoad 0.6.0
and XOVI 0.3.3. Other hardware and firmware are unverified.

The IMAP preview connects directly to compatible mail providers. Gmail users with app passwords can connect without setting up a Cloud project.

See [security and privacy](SECURITY.md) for data handling, private vulnerability
reports, and release checks. Never attach account files or real mail to an issue.

Source is licensed under GPL-3.0-or-later; see [LICENSE](LICENSE).
