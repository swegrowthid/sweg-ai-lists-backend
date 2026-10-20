# Plan: Login Magic Link (Email)

> Status (2026-10-20, round 2): fitur sudah merged ke `origin/main` (`792f4d2`).
> Copy email diselaraskan dengan frontend: English, brand `swegai` (`SITE.name`
> di `src/data/site.ts`), sender `swegai <noreply@ai-sweg.my.id>`. Copy final
> ada di `internal/auth/magic.go`; bagian desain di bawah adalah rekam
> perancangan.

## Context

- Login manual sudah ada: `POST /auth/login` (identifier + password), JWT HS256,
  refresh rotation, logout. Plan lama: `plans/login-auth.md`.
- User mau dua opsi login: manual (tetap) dan magic link (baru).
- Email provider: JetEmail transactional API.
- Backend API-only. Frontend Astro statis di `https://ai-sweg.my.id`.
- OpenAPI di `api/openapi.yaml` tetap satu sumber kontrak.
- Riset Exa dipakai untuk pola magic link di Go dan praktik keamanannya.

## Riset Exa

Pola magic link yang dipakai proyek Go nyata (Calnode, runer-api, goauth,
magiclink-auth-go) selalu sama:

- Token satu pakai: 32 byte `crypto/rand`, base64url. Entropi minimal 128 bit.
- Database menyimpan SHA-256 hash token saja. Bocornya DB tidak menghasilkan
  token yang bisa dipakai.
- TTL pendek: 10-15 menit.
- Konsumsi atomik: satu `UPDATE ... WHERE used_at IS NULL AND expires_at > now()`,
  cek `RowsAffected == 0`. Dua request bersamaan tidak bisa dua-duanya menang.
- Endpoint minta selalu jawab 200 dengan pesan sama, email terdaftar atau tidak.
  Ini anti enumerasi akun (pola Stytch, Calnode).
- Rate limit permintaan: 3-5 per email per jam (checkyourvibe, AuthOS).
- Email scanner korporat (Outlook SafeLinks, Mimecast) mem-fetch link sebelum
  manusia klik. GET yang langsung login akan membakar token satu pakai. Kasus
  nyata: coneshare issue #224, oma issue #254. Solusi standar: GET hanya
  menampilkan halaman konfirmasi, POST dari aksi user yang menghabiskan token
  (pushery security model, chassis guide, devobs).
- Link + kode 6 digit dalam satu email adalah pola bonus untuk lintas device
  (chassis). Tidak wajib di v1.

JetEmail:

- Endpoint kirim: `POST https://api.jetemail.com/email`, header
  `Authorization: Bearer transactional_...`. Body JSON: `from`, `to`, `subject`,
  `html` dan/atau `text`.
- Header `Idempotency-Key` (1-256 char) bikin retry aman: kirim ulang dengan key
  dan body sama membalas hasil asli, tidak dobel kirim.
- Rate limit transactional: 100 email/menit tier free. Jauh di atas kebutuhan.
- Domain kirim harus diverifikasi di dash.jetemail.com: SPF, DKIM, DMARC.
- SDK Go resmi ada: `github.com/jetemail/jetemail-go`. Tapi API-nya satu
  endpoint dan SDK-nya baru satu commit, jadi klien kecil `net/http` tanpa dep
  baru lebih pas dengan gaya stdlib repo ini.

Sumber:

- https://jetemail.com/docs/api-reference/email/send-an-email
- https://jetemail.com/docs/api-reference/introduction
- https://jetemail.com/docs/transactional-email/getting-started
- https://github.com/jetemail/jetemail-go
- https://github.com/Calnode/calnode/blob/main/internal/handler/magic_link.go
- https://github.com/runernotes/runer-api (magic link + refresh token)
- https://github.com/teslashibe/magiclink-auth-go
- https://github.com/himself65/auth-spec/blob/main/skills/security-best-practice/rules/token-lifecycle.md
- https://docs.pushery.com/email-magic-link-for-laravel/security-model/
- https://www.devobs.io/articles/magic-link-replay-and-scanners/
- https://github.com/coneshare/coneshare/issues/224
- https://github.com/duyet/oma/issues/254
- https://github.com/dvd90/chassis/blob/master/docs/guides/magic-link.md
- https://checkyourvibe.dev/blog/how-to/magic-links
- https://stytch.com/docs/resources/policies/platform/account-enumeration
- https://authos.dev/docs/api/reference/authentication/magic-link/

## Keputusan (final)

- Dua langkah, dua endpoint:
  1. `POST /auth/magic-link` `{email}` - minta link. Selalu 200 + pesan generik.
  2. `POST /auth/magic-link/verify` `{token}` - tukar token jadi `TokenPair`.
     Kontrak response sama persis dengan login, jadi `savePair` frontend tidak
     berubah.
- Link di email menunjuk halaman frontend `MAGIC_LINK_BASE_URL?token=...`,
  bukan endpoint backend. Halaman itu konfirmasi: GET tidak pernah consume.
  Tukar token terjadi lewat POST dari klik user di halaman itu. Ini yang bikin
  scanner prefetch tidak membakar token.
- Token: 32 byte `crypto/rand`, base64url tanpa padding (43 char). DB simpan
  hex(SHA-256(token)) sebagai primary key. Token mentah hanya ada di email.
- TTL default 15 menit, env `MAGIC_LINK_TTL` (Go duration).
- Single-use: satu UPDATE kondisional, `RowsAffected == 0` berarti tidak valid,
  kadaluarsa, atau sudah dipakai. Semua jawab 401 pesan sama.
- Rate limit: maksimal 5 link per email per jam, hitung baris `magic_links`.
  Lebih dari itu jawab 429. Tanpa dep baru.
- Hanya email terdaftar yang dikirim link. Tanpa auto-register; user baru tetap
  lewat `POST /users/register` supaya pilih username sendiri.
- Email kirim via JetEmail REST dengan klien `net/http` kecil. Tanpa dep baru.
  `Idempotency-Key` = hash token, jadi retry kirim tidak dobel.
- Tanpa `JETEMAIL_API_KEY`: dev dan test pakai `LogMailer` (link ke log). Di
  prod (`APP_ENV=prod`) key kosong = panic saat boot, seperti `JWT_SECRET`.
- Login manual tidak berubah sama sekali.

## Call graph (draf)

```text
POST /auth/magic-link (publik)
  -> auth.Handler.requestMagicLink
    -> decode and validate MagicLinkRequest
    -> auth.Magic.Request
      -> MagicStore.CountRecent(email, 1 jam) -> 429 bila >= 5
      -> user.Store.FindByEmail
        -> tidak ada: selesai diam (tetap 200)
        -> ada: buat token 32B, simpan SHA-256 hash + TTL
          -> MagicStore.Save
            -> DELETE baris kadaluarsa lama (opportunistik)
          -> mailer.Mailer.Send (JetEmail POST /email)
      -> 200 {"message": "..."} selalu sama

halaman frontend /login/magic?token=... (GET hanya render)
  -> klik "sign in" -> POST /auth/magic-link/verify {token}
    -> auth.Handler.verifyMagicLink
      -> auth.Magic.Verify
        -> hex(SHA-256(token))
        -> MagicStore.Consume (UPDATE used_at, atomic)
        -> auth.Tokens.Issue + RefreshStore.Save
      -> 200 TokenPair (kontrak sama dengan /auth/login)
```

## Skema SQL (draf)

```sql
-- +goose Up
CREATE TABLE magic_links (
  token_hash TEXT PRIMARY KEY,
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  email      TEXT NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  used_at    TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_magic_links_email_created
  ON magic_links (email, created_at);

-- +goose Down
DROP TABLE IF EXISTS magic_links;
```

- Migrasi additif baru `00009`. Migrasi 00001-00008 tidak disentuh.
- Rate limit = `COUNT(*) WHERE email = $1 AND created_at > now() - interval '1 hour'`.
- Baris basi (used atau expired lebih dari 24 jam) dihapus opportunistic saat
  `Save`. Tabel kecil, tanpa pekerja latar.

## Kontrak endpoint (draf)

`POST /auth/magic-link` - publik.

```json
// request
{"email": "budi@test.id"}
// 200 selalu, email terdaftar atau tidak
{"message": "if the email is registered, a login link is on its way"}
// 429 saat lebih dari 5 permintaan per email per jam
"too many requests"
// 400 body tidak valid
"invalid request body"
```

`POST /auth/magic-link/verify` - publik.

```json
// request
{"token": "43-char-base64url"}
// 200 - bentuk sama dengan /auth/login
{"access_token": "...", "token_type": "Bearer", "expires_in": 86400,
 "refresh_token": "...", "refresh_expires_in": 604800}
// 401 satu pesan untuk: token tidak dikenal, kadaluarsa, atau sudah dipakai
"invalid or expired link"
// 400 body tidak valid
"invalid request body"
```

Isi email (html + text):

- Subjek: `Tautan login SWEG-AI`.
- Link: `{MAGIC_LINK_BASE_URL}?token={token}`.
- Keterangan: tautan berlaku 15 menit dan satu kali pakai.
- Baris keamanan: abaikan bila bukan yang minta.

## Kontrak frontend (ikutan, project terpisah)

- Halaman login dapat opsi kedua: masukkan email, panggil
  `POST /auth/magic-link`, tampilkan pesan cek email.
- Halaman baru `/login/magic` membaca `?token=`. GET halaman tidak consume.
  Tombol "sign in" memanggil `POST /auth/magic-link/verify`, lalu `savePair`
  dan redirect, kontrak sama dengan login sekarang.
- Halaman itu perlu `Referrer-Policy: same-origin` supaya URL berisi token
  tidak bocor lewat header Referer.
- Tanpa JS, halaman tetap jelas: tombol submit form biasa.

## Files to modify

- `migrations/00009_create_magic_links.sql` - tabel magic_links baru.
- `internal/auth/magic.go` - baru: port `MagicStore`, service `Magic`
  (Request, Verify), generator token, hash SHA-256.
- `internal/auth/store_magic_postgres.go` - baru: Save, Consume, CountRecent.
- `internal/auth/store_magic_memory.go` - baru: padanan in-memory untuk test.
- `internal/auth/http.go` - dua handler baru + `SetMagic` pada Handler.
- `internal/user/user.go` + `service.go` + `store_postgres.go` +
  `store_memory.go` - tambah `FindByEmail` (satu method baca kecil).
- `internal/platform/mailer/mailer.go` - baru: port `Mailer` + `LogMailer`.
- `internal/platform/mailer/jetemail.go` - baru: klien JetEmail `net/http`.
- `internal/app/app.go` - wiring mailer + Magic + route.
- `internal/platform/config/config.go` - env baru (bawah).
- `api/openapi.yaml` - dua path baru + skema request + catatan keamanan.
- `.env-example` - contoh env baru.
- `README.md` dan `deploy/README.md` - env prod + setup domain JetEmail.
- `go.mod`/`go.sum` - tidak berubah (tanpa dep baru).

Env baru:

- `JETEMAIL_API_KEY` - key transactional (`transactional_...`). Kosong = dev
  LogMailer; kosong di prod = panic boot.
- `MAIL_FROM` - misal `swegai <noreply@ai-sweg.my.id>`. Wajib saat kirim
  aktif.
- `MAGIC_LINK_TTL` - default `15m`.
- `MAGIC_LINK_BASE_URL` - URL halaman frontend, misal
  `https://ai-sweg.my.id/login/magic`. Wajib di prod.

## Reuse

- `auth.Tokens.Issue` + `RefreshStore.Save` - verify mengeluarkan pair lewat
  jalur yang sama dengan login, jadi rotation dan reuse detection otomatis.
- Pola 4 peran paket domain: entity/service/store/http.
- `decodeJSON` di `internal/auth/http.go` - batas body + tolak field asing.
- `config.Load` tetap baca env sekali di edge.
- `errors.Is` untuk klasifikasi error; pesan 401 generik anti enumerasi.
- Memory store fallback saat pool nil, seperti store lain.
- `cmd/memserver` untuk E2E lokal dengan LogMailer.

## Steps

- [ ] Kunci keputusan di Open questions dengan user.
- [ ] Setup JetEmail: domain kirim + SPF/DKIM/DMARC + key transactional
      (langkah manusia di dash.jetemail.com).
- [x] Tulis migrasi 00009.
- [x] Implementasi `MagicStore` Postgres + memory.
- [x] Implementasi service `Magic` (Request, Verify) + generator token.
- [x] Implementasi port `Mailer` + klien JetEmail + LogMailer.
- [x] Tambah `FindByEmail` di user store.
- [x] Tambah dua handler + wiring di app + config env.
- [x] Selaraskan OpenAPI dan `.env-example`.
- [x] Test: request selalu 200 sama, 429 rate limit, verify happy path,
      verify unknown/expired/used satu pesan 401, race dua verify bersamaan,
      token tidak pernah ke log, klien JetEmail dengan httptest.

## Verification

- `go vet ./...` dan `go test ./...` bersih.
- `cmd/memserver`: minta link email apa pun -> 200 pesan sama, link muncul di
  log (LogMailer).
- Verify token dari log -> 200 TokenPair; login Bearer jalan.
- Verify token sama kedua kali -> 401.
- Verify token ngarang -> 401 pesan sama.
- 6 permintaan email sama dalam 1 jam -> permintaan ke-6 jawab 429.
- Email terdaftar dan tidak terdaftar: response byte-identik.
- Prod: key kosong gagal boot dengan pesan jelas.

## Open questions

- Link hanya untuk email terdaftar (tanpa auto-register) - setuju?
- Kode 6 digit sebagai cadangan lintas device - v1 atau v2? Rekomendasi: v2.
- Halaman frontend `/login/magic` dan domain `https://ai-sweg.my.id` - path
  ini benar?
- Pengirim `swegai <noreply@ai-sweg.my.id>` - benar?
- TTL 15 menit dan rate limit 5/jam per email - angka final?
- Perlu simpan IP peminta di tabel untuk audit, atau skip v1? Rekomendasi:
  skip; RemoteAddr di belakang reverse proxy menyesatkan.
