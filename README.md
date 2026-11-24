# Silo Request Notifier

Send an ntfy push when someone submits a movie or series request in Silo. The alert includes the title, year, and requester when available. Requests that need approval and requests that are auto-approved both trigger alerts at submission.

## Install from Silo

Open **Admin → Integrations → Plugins → Catalog → Repositories → Add repository**. Enter **Silo Request Notifier** as the name and use this catalog URL.

```text
https://raw.githubusercontent.com/barolinecrewer/silo-request-notifier/main/catalog.json
```

Find **Silo Request Notifier** in the plugin catalog and install it. Builds are available for Linux x86-64 and ARM64.

## Configure alerts

1. Set up a private ntfy topic such as `silo-requests` and subscribe to it in the ntfy app. Create an access token for a user with permission to publish to that topic.
2. In the plugin's settings, enter your HTTPS ntfy server URL, topic, and publishing token. Generate a separate token with `openssl rand -hex 32` and enter it as **Webhook token**.
3. In **Admin → Settings → Notifications → Server Channels**, add a **Generic** channel with this URL. Replace `SILO_HOST`, `INSTALLATION_ID`, and `WEBHOOK_TOKEN`.

   ```text
   https://SILO_HOST/api/v2/plugin-content/plugins/INSTALLATION_ID/requests?token=WEBHOOK_TOKEN
   ```

   In Silo's authenticated `GET /api/v2/admin/plugins/installations` response, find the entry whose `presentation.display_name` is **Silo Request Notifier** and use its `id`.
4. Enable **Request submitted** for this channel and turn its other event switches off. Run the channel's **Test** action. The ntfy app should show **Silo request notifier test**.

Silo's plugin proxy filters webhook signature headers, so the webhook URL carries the credential. Keep the URL private and redact its query string in access logs. If the token is exposed, rotate it in the plugin settings and channel URL.

Silo sends submission webhooks on a best-effort basis. A delivery that fails during an outage may be missed.

## Build

```sh
go test ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o silo-request-notifier-linux-amd64 .
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o silo-request-notifier-linux-arm64 .
```
