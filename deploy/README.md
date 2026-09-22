# Deploy sweg-ai lists backend

Pipeline mengirim biner ke server lewat SSH. Migrasi jalan sebagai unit
systemd oneshoot. API jalan sebagai service systemd. Tanpa Docker.

## Berkas

- `.github/workflows/ci.yml` - CI (vet, test, build) dan CD (deploy).
- `deploy/sweg-ai.service` - unit service API.
- `deploy/sweg-ai-migrate.service` - unit migrasi (`goose up`).
- `deploy/sudoers` - izin sudo terbatas untuk user deploy.

## Setup server (sekali jalan)

Asumsi: Ubuntu/Debian dengan systemd. Pastikan `rsync` dan `curl` terpasang.

1. Buat user system untuk service:

   ```sh
   useradd --system --home /opt/sweg-ai \
     --shell /usr/sbin/nologin sweg-ai
   ```

2. Buat direktori. User deploy memiliki `/opt/sweg-ai`:

   ```sh
   install -d -o deploy -g deploy -m 755 /opt/sweg-ai
   install -d -o root -g root -m 755 /etc/sweg-ai
   ```

3. Tulis `/etc/sweg-ai/env` dengan mode 600 milik root. Isi lihat
   `.env-example`: `DB_URL`, `JWT_SECRET`, `JWT_ACCESS_TTL`,
   `JWT_REFRESH_TTL`. Tambah `APP_ENV=prod` dan `APP_ADDR=:8080`.
   Set `DOCS_UI=1` bila UI Scalar di `/docs` mau hidup di prod.

4. Pasang unit service:

   ```sh
   install -m 644 deploy/sweg-ai.service /etc/systemd/system/
   install -m 644 deploy/sweg-ai-migrate.service /etc/systemd/system/
   systemctl daemon-reload
   systemctl enable sweg-ai.service
   ```

5. Pasang sudoers. Ganti user `deploy` di `deploy/sudoers` dengan user SSH
   deploy kamu. Lalu pasang:

   ```sh
   visudo -cf deploy/sudoers
   install -m 440 deploy/sudoers /etc/sudoers.d/deploy-sweg-ai
   ```

6. Amankan jaringan. Batasi port 8080 ke reverse proxy saja. Contoh Caddy:
   `reverse_proxy 127.0.0.1:8080`.

## Konfigurasi GitHub

Set secrets di Settings > Secrets and variables > Actions:

| Secret | Wajib | Nilai |
| --- | --- | --- |
| `DEPLOY_HOST` | ya | alamat server |
| `DEPLOY_USER` | ya | user SSH deploy, mis. `deploy` |
| `DEPLOY_SSH_KEY` | ya | private key ED25519 user deploy |
| `DEPLOY_PORT` | tidak | default `22` |
| `DEPLOY_PATH` | tidak | default `/opt/sweg-ai` |

Letakkan public key di `~/.ssh/authorized_keys` user deploy.
Environment `production` dibuat otomatis oleh workflow. Tambahkan
protection rules di environment itu bila perlu tinjauan manual.

## Alur deploy

- Push ke `main` atau pull request memicu job `checks`.
- Push ke `main` memicu job `deploy` setelah `checks` lulus.
- Urutan: rsync biner + `migrations/` + `release.env`, lalu migrasi,
  lalu restart API, lalu cek `http://127.0.0.1:8080/readyz`.
- Jika migrasi gagal, deploy berhenti. API lama tetap jalan.
- Jika cek kesehatan gagal, pipeline merah. Periksa server dengan
  `systemctl status sweg-ai` dan `journalctl -u sweg-ai`.

## Catatan

- Rollback: jalankan ulang workflow dari commit lama, atau `git revert`.
- `APP_VERSION` terisi otomatis dari git SHA via `release.env`.
- Bila `APP_ADDR` bukan port 8080, sesuaikan langkah `Health check`
  di `.github/workflows/ci.yml`.
