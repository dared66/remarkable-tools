# Install reMail on reMarkable 2

This is an experimental community app. The tested combination is **reMarkable 2,
firmware 3.29.1.0, AppLoad 0.6.0, XOVI 0.3.3, and qt-resource-rebuilder 19.0.1**.
The ARMv7 download is not for Paper Pro or Paper Pro Move. Firmware updates can
break unofficial apps. Keep important drafts backed up elsewhere.

The tablet uses Wi-Fi to talk directly to Gmail. A **Mac with Python 3.10+** is
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
[reMail v0.1.0](https://github.com/dared66/remarkable-tools/releases/tag/remail-v0.1.0)
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
launcher and open **reMail**. A Gmail connection screen is expected until step 4.

## 3. Create your own Gmail connection

No shared OAuth client, account, or developer Gmail access is bundled. Each
installer uses their **own Google Cloud project and Desktop OAuth client**.

1. Open [Google Cloud Console](https://console.cloud.google.com/), create or select
   a project, and enable **Gmail API**. Billing is not needed for this app's setup.
2. Open Google Auth Platform. Set an app name, support email, developer contact,
   and **External** audience. For personal testing, add your Gmail address as a
   test user. Review and accept Google's policies yourself.
3. Under Data Access, add only
   `https://www.googleapis.com/auth/gmail.modify`. It permits reading, sending,
   and changing labels without immediate permanent deletion. The app provides
   no message-delete action.
4. Create a **Desktop app** client under Clients. Download the JSON and save it
   as `.state/gmail-client.json`. Run `chmod 600 .state/gmail-client.json`.
5. Testing-issued refresh tokens normally expire after seven days. For ongoing
   personal use, configure Production in your project, supply the branding
   information Google requests, and sign in again after switching. Use branding
   and domains you own; this project's website is not your verified domain.
   [Google's personal-use exception](https://developers.google.com/identity/protocols/oauth2/production-readiness/restricted-scope-verification#personal-use)
   allows limited personal use without verification, with an unverified warning
   and user cap. Publicly offering a shared OAuth service needs Google's
   applicable verification. This source release does not provide that service.

Do not commit your client JSON, tokens, SSH keys, or mail. The ignored `.state/`
directory is for local configuration, not publication.

## 4. Sign in and transfer account access

Run from the repository root in a normal Mac Terminal:

```sh
python3 -m venv .venv
.venv/bin/python -m pip install -r mail/requirements-setup.txt
.venv/bin/python mail/scripts/account.py connect --client .state/gmail-client.json
.venv/bin/python mail/scripts/account.py install
```

Complete Google's sign-in and Gmail consent yourself in the browser. The tool
uses PKCE and a temporary loopback callback. It stores credentials in macOS
Keychain and transfers them directly through SSH; it does not write a local
token file. A Keychain access prompt may appear. The Mac setup tool supports
macOS only and refuses a plaintext credential-store fallback.

Open reMail and tap **Check connection** or **Refresh**. You should see your
Gmail inbox. Afterward the Mac and USB cable are not required; keep tablet Wi-Fi
connected. The optional read-only check reports counts without mail content:

```sh
python3 mail/scripts/device_live_check.py
```

## Updates, removal, and troubleshooting

- **Update:** save drafts and close reMail, verify the new release checksum, then
  repeat step 2 with its extracted `RemarkMail` directory. Close and reopen it
  to load the new interface. If a release changes the backend, restart the tablet
  and activate XOVI through your usual launcher so the old idle backend is replaced.
  Account data is stored separately and is preserved.
- **Google sign-in expires:** repeat step 4. If the project is still in Testing,
  seven-day expiry is expected. Other revocation/expiry reasons still apply in
  Production. See [Google's token policy](https://developers.google.com/identity/protocols/oauth2#expiration).
- **SSH fails:** reconnect/unlock the tablet, check its address and key, and test
  `ssh -F .state/ssh_config remarkable-usb` interactively first. Do not paste
  credentials into an issue.
- **Inbox will not refresh:** verify Wi-Fi and tablet date/time. The app may show
  cached messages while offline. Read the visible error before reconnecting.
- **Uncertain send:** check Gmail's Sent folder before sending another copy.
- **Remove:** close reMail and remove only its application directory,
  `/home/root/xovi/exthome/appload/RemarkMail`. This preserves local drafts and
  cached mail at `/home/root/.remarkmail`. To remove those too, deliberately
  remove that separate directory after backing up anything you want to keep.
  Delete the `local.skills.remarkable-mail` / `default` entry in macOS Keychain
  Access and revoke the app at [Google Account connections](https://myaccount.google.com/connections).
  Revoking access does not erase existing local copies.

Tablet credentials have mode `0600` and the state directory mode `0700`, but are
**not encrypted by reMail**. Keep the device secure. See the
[privacy policy](https://krupapc.dev/apps/remail/privacy/).
