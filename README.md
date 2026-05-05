# Trippy Track

<p align="center">
  <img src="screenshot.png" alt="Trippy Track" height="360">
</p>

<p align="center"><b>Self-hosted travel journal with live GPS tracking, photo timeline, and a map your friends can follow.</b></p>

## How it works

1. **You travel.** [OwnTracks](https://owntracks.org/) runs on your phone and silently reports your GPS position.
2. **You capture moments.** Snap photos, record videos, write short entries — right from the web UI.
3. **You share a link.** Friends open it and see your route on a live map, with your timeline of photos and entries.
4. **They stay in the loop.** The map updates in real time. New entry? They get a push notification.

That's it. Your own travel blog that writes itself as you go.

## Features

- Real-time GPS tracking with live map updates (SSE)
- Journal entries with photos, location, and weather
- Photo clustering on the map
- Shareable trip links for followers
- Web Push notifications for trip updates
- OIDC authentication
- SQLite database, single binary deployment

## Configuration

Set the following environment variables (or use a `.env` file):

| Variable             | Description                              | Default                          |
|----------------------|------------------------------------------|----------------------------------|
| `PORT`               | Server port                              | `8080`                           |
| `DATABASE_URL`       | Path to SQLite database                  | `trippy-track.db`                |
| `UPLOADS_DIR`        | Photo upload directory                   | `uploads`                        |
| `OIDC_ISSUER_URL`    | OIDC provider URL                        |                                  |
| `OIDC_CLIENT_ID`     | OIDC client ID                           |                                  |
| `OIDC_CLIENT_SECRET` | OIDC client secret                       |                                  |
| `OIDC_REDIRECT_URL`  | OAuth callback URL                       | `http://localhost:8080/callback` |
| `VAPID_PUBLIC_KEY`   | VAPID public key for push notifications  |                                  |
| `VAPID_PRIVATE_KEY`  | VAPID private key for push notifications |                                  |
| `VAPID_CONTACT`      | VAPID contact email/URL                  |                                  |

## Building

```bash
nix build
```
