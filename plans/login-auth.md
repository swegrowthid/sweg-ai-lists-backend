# Plan: Login + Authentication + Authorization

## Context

- Register sudah ada: `POST /users/register`.
- Login belum ada. Token belum ada.
- Middleware auth belum ada.
- `GET /users` masih terbuka untuk publik.
- Tidak ada kolom peran. Semua user setara, tanpa RBAC.
- Tujuan: user login lalu akses resource dengan token.

## Approach (final)

- Modul baru `internal/auth`: token issuer, validator, middleware, context helper.
- Login fleksibel: satu field identifier terima username atau email.
- Verifikasi password dengan `bcrypt.CompareHashAndPassword`.
- Token JWT stateless HS256 via `golang-jwt/jwt` v5.
- Terbitkan access token singkat + refresh token tahan lama.
- Tambah refresh dan logout sekarang, bukan tahap dua.
- Middleware auth: baca `Authorization: Bearer`, validasi, isi `context`.
- Otorisasi: hanya `RequireAuth`. Tanpa cek peran.
- Route: `POST /auth/login` publik. `POST /users/register` tetap publik.
- `GET /users` wajib login. Semua user login boleh akses.
- Secret dari env baru `JWT_SECRET`. Baca sekali di `config.Load`.
- Samakan kontrak di `api/openapi.yaml`.

Call graph (draf):

```text
POST /auth/login
  -> auth.Handler.login
    -> decode and validate LoginRequest
      -> user.Service.Authenticate
        -> user.Store.FindByUsernameOrEmail
        -> bcrypt.CompareHashAndPassword
      -> auth.TokenIssuer.Issue
        -> sign access token + refresh token

GET /users (wajib login)
  -> auth.Middleware.RequireAuth
    -> parse and verify Bearer token
      -> user.Handler.list
```

## Files to modify

- `internal/user/user.go` - error login, bentuk `LoginInput`.
- `internal/user/service.go` - fungsi `Authenticate`.
- `internal/user/store_postgres.go` - fungsi `FindByUsernameOrEmail`.
- `internal/user/store_memory.go` - fungsi yang sama untuk test.
- `internal/auth/*` (baru) - token issuer, validator, middleware, context helper.
- `internal/app/app.go` - wiring auth dan route terproteksi.
- `internal/platform/config/config.go` - `JWT_SECRET` dan umur token.
- `api/openapi.yaml` - skema login, refresh, logout, keamanan Bearer.
- `go.mod` + `go.sum` - tambah `golang-jwt/jwt` v5.
- `.env-example` - tambah `JWT_SECRET=` dan umur token.

## Reuse

- `user.Store` tetap port persistence. Tambah method baca, bukan repo baru.
- `user.Service` tetap pemilik use case user.
- `user.Handler` tetap pemilik status HTTP user.
- `bcrypt` tetap untuk verifikasi password.
- `config.Load` tetap baca env sekali di edge.
- `app.withLogging` jadi pola untuk middleware baru.
- `errors.Is` + `errors.As` tetap untuk klasifikasi error.
- Pesan duplicate generik tetap jadi pola anti enumerasi user.

## Steps

- [x] Kunci identifier login, jenis token, model otorisasi.
- [x] Tambah `FindByUsernameOrEmail` pada `Store` + Postgres + memory.
- [x] Tambah `Authenticate` pada service dengan pesan error generik.
- [x] Tambah penerbit dan validasi JWT HS256.
- [x] Tambah endpoint refresh token.
- [x] Tambah endpoint logout.
- [x] Tambah middleware auth + helper context user.
- [x] Kunci `GET /users` wajib login.
- [x] Tambah route `POST /auth/login` publik.
- [x] Selaraskan OpenAPI dan `.env-example`.
- [x] Tambah test: login sukses, password salah, token kadaluarsa, tanpa token, refresh, logout.

## Verification

- `go vet ./...` bersih.
- `go test ./...` lulus.
- Login valid balas 200 + token. Response tanpa hash.
- Login salah balas 401 dengan pesan generik.
- Akses tanpa token balas 401.
- Token kadaluarsa balas 401.
- Refresh valid balas 200 + token baru.
- Logout invalidasi refresh lalu pakai balas 401.
- Cek `GET /users` sesudah proteksi.

## Open questions

Semua kunci 2026-09-21:

- Identifier: fleksibel, username atau email.
- Token: JWT stateless HS256.
- Scope: sekalian refresh token + logout.
- Otorisasi: semua user setara, tanpa RBAC.
- `GET /users`: semua user login.
- Route: `POST /auth/login`.
- Transport: header `Authorization: Bearer`.
- Secret: `JWT_SECRET` via env.

Catatan implementasi: logout JWT stateless butuh keputusan teknik
(denylist singkat atau rotasi refresh) saat eksekusi.
