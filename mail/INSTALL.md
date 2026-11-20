# Install reMail on reMarkable 2

This is an experimental community app. The tested combination is **reMarkable 2,
firmware 3.29.1.0, AppLoad 0.6.0, XOVI 0.3.3, and qt-resource-rebuilder 19.0.1**.
The ARMv7 download is not for Paper Pro or Paper Pro Move. Firmware updates can
break unofficial apps. Keep important drafts backed up elsewhere.

The tablet connects to your provider over IMAP and SMTP using Wi-Fi. A **Mac with Python 3.10+** is
currently needed for account setup. It can be disconnected after setup.

## 1. Prepare AppLoad and SSH

Install [XOVI](https://github.com/asivery/xovi),
[qt-resource-rebuilder](https://github.com/asivery/rm-xovi-extensions/tree/master/qt-resource-rebuilder),
and [AppLoad](https://github.com/asivery/rm-appload) using their maintainers'
instructions for your firmware. This release does not install or modify those
components. Check that AppLoad opens before proceeding.

Connect the tablet by USB and find its SSH password under Settings → Help →
Copyrights and licenses. The usual USB address is `10.11.99.1`. From your Mac:

```sh
git clone https://github.com/dared66/remarkable-tools.git
cd remarkable-tools
mkdir -p .state downloads
chmod 700 .state
ssh root@10.11.99.1
```

Verify the device's SSH fingerprint when first connecting, enter its password,
then type `exit`. Set up an SSH key if you do not already have one for the tablet:

```sh
ssh-keygen -t ed25519 -f "$HOME/.ssh/remarkable-tools" -C remarkable-tools
cat "$HOME/.ssh/remarkable-tools.pub" | ssh root@10.11.99.1 'umask 077; mkdir -p ~/.ssh; cat >> ~/.ssh/authorized_keys'
ssh-add "$HOME/.ssh/remarkable-tools"
```

Save the following as `.state/ssh_config` inside the cloned repository. If you
already have a tablet key, use its path in `IdentityFile` instead.

```sshconfig
Host remarkable-usb
    HostName 10.11.99.1
    User root
    IdentityFile ~/.ssh/remarkable-tools
    IdentitiesOnly yes
    ConnectTimeout 10
```

Confirm `ssh -F .state/ssh_config remarkable-usb 'uname -m'` reports `armv7l`.
Leave normal SSH host-key checking enabled. The account-transfer step needs
key authentication, because its input pipe carries account data. The setup tool
requires the previously verified host key and disables SSH agent forwarding.

## 2. Download and install the app

Download **RemarkMail-rm2.tar.gz** and **SHA256SUMS.txt** from
[reMail v0.2.0 preview](https://github.com/dared66/remarkable-tools/releases/tag/remail-v0.2.0-rc1)
into `downloads/` in the clone. This is a prebuilt app; Go and Qt are not needed
to install it.

```sh
cd downloads
shasum -a 256 -c SHA256SUMS.txt
tar -xzf RemarkMail-rm2.tar.gz
cd ..
ssh -F .state/ssh_config remarkable-usb 'test -d /home/root/xovi/exthome/appload'
scp -O -F .state/ssh_config -r downloads/RemarkMail remarkable-usb:/home/root/xovi/exthome/appload/
ssh -F .state/ssh_config remarkable-usb 'chmod 755 /home/root/xovi/exthome/appload/RemarkMail/backend/entry'
```

Stop if the checksum fails or the AppLoad directory is missing. Refresh AppLoad's
launcher and open **reMail**. New installations show the account connection screen. The IMAP release has been tested with local TLS mail servers and cross-built for ARMv7. The ARM backend passed a synthetic AppLoad socket check on a physical reMarkable 2. Live IMAP/SMTP provider testing is pending.

## 3. Connect your mailbox

In the cloned repository, create the Mac setup environment:

```sh
python3 -m venv .venv
.venv/bin/python -m pip install -r mail/requirements-setup.txt
```

For a personal Gmail account with two-step verification and [App Passwords](https://support.google.com/accounts/answer/185833) available, generate a password for reMail and run:

```sh
.venv/bin/python mail/scripts/account.py connect --gmail --email you@gmail.com
```

Type the app password at the hidden prompt. This setup needs no Google Cloud project. Some managed accounts and accounts with Advanced Protection cannot create app passwords.

For another provider, substitute its server names, mailbox address, and exact folder names. IMAP uses implicit TLS on port 993. SMTP uses implicit TLS on 465 by default, or required STARTTLS on 587. The server must accept IMAP login and SMTP AUTH PLAIN over TLS; OAuth-only accounts are unsupported on this path.

```sh
.venv/bin/python mail/scripts/account.py connect --email you@example.org \
  --imap-host imap.example.org --smtp-host smtp.example.org --smtp-port 587 \
  --sent-folder Sent --archive-folder Archive
```

Use `--username` when the login differs from the email address. Gmail defaults to English folder names; override them for localized mailboxes. Setup verifies both logins without sending a message and saves the account in Mac Keychain.

## 4. Transfer the account and open reMail

Close reMail before switching accounts. Save any open draft first. With the tablet connected over USB, run:

```sh
.venv/bin/python mail/scripts/account.py install
```

Open reMail, tap **Check connection**, and refresh Inbox over Wi-Fi. The password stays in a private file in the tablet's RAM-backed /run directory. After a tablet reboot, rerun the install command to restore access from Keychain. Messages and drafts remain in account-specific storage. Switching from the older OAuth setup starts a separate IMAP cache and draft list.

## Updates, removal, and troubleshooting

- **Update:** save drafts and close reMail, verify the new release checksum, then
  repeat step 2 with its extracted `RemarkMail` directory. Close and reopen it
  to load the new interface. If a release changes the backend, restart the tablet
  and activate XOVI through your usual launcher so the old idle backend is replaced.
  Account data is stored separately and is preserved.
- **Password revoked:** create a replacement with your mail provider and repeat steps 3 and 4.
- **SSH fails:** reconnect/unlock the tablet, check its address and key, and test
  `ssh -F .state/ssh_config remarkable-usb` interactively first. Do not paste
  credentials into an issue.
- **Inbox will not refresh:** verify Wi-Fi and tablet date/time. The app may show
  cached messages while offline. Read the visible error before reconnecting.
- **Uncertain send:** check the sending account before retrying; some SMTP providers do not save a Sent copy before sending another copy.
- **Remove:** close reMail and remove only its application directory,
  `/home/root/xovi/exthome/appload/RemarkMail`. This preserves local drafts and
  cached mail at `/home/root/.remarkmail`. To remove those too, deliberately
  remove that separate directory after backing up anything you want to keep.
  Delete the `local.skills.remarkable-mail` / `default` entry in macOS Keychain
  Access and revoke the app password with your provider. Reboot to clear the tablet password.
  Revoking access does not erase existing local copies.

Cached mail and drafts are private files, unencrypted on the tablet. A process with root access can read them and the active session password.
