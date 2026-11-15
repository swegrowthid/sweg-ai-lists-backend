# Plan: Identity linking completion - set password pertama + unlink

## Context

- Approach yang diminta (OAuth login -> cek identity -> cek user existing
  via email -> link atau buat baru; password lokal tetap jalan) sudah jalan
  di commit `f6e5a0a` (`feat/enhance-auth-feature`), tabel `user_identities`
  (migrasi 00014).
- Yang sudah hidup sekarang:
  - OAuth dulu -> `POST /auth/supabase` membuat user tanpa password,
    username dari local-part email.
  - Register lokal dulu -> OAuth dengan email sama -> identity ditautkan,
    password dan post tetap milik user lama.
  - Register lokal dengan email yang sudah tertaut swegrowth.id -> gate 409.
- Dua gap yang bikin "bisa masuk lewat semua cara" belum penuh:
  1. User oauth-first punya `password_hash` kosong. `PUT /users/password`
     mewajibkan `current_password`, jadi mereka tidak pernah bisa membuat
     password. Email + password tidak pernah bisa dipakai.
  2. Tautan provider hanya bisa bertambah (lewat login pertama), tidak bisa
     dilepas. Tidak ada DELETE.
- Tanpa migrasi: kolom dan tabel sudah ada.

## Approach (final)

- `PUT /users/password` tetap satu endpoint, semantik diperluas:
  - `password_hash` kosong + `current_password` kosong -> set password
    pertama. Pemanggil sudah terautentikasi Bearer, jadi tidak ada yang
    perlu diverifikasi; sama dengan kebijakan "set password" GitHub.
  - `password_hash` kosong + `current_password` terisi -> 401
    `current password is incorrect` (bukan 400 seperti sekarang).
  - `password_hash` terisi -> perilaku lama: wajib `current_password` benar;
    `current_password` kosong juga 401 (satu kelas dengan password salah).
  - Sukses tetap revoke semua refresh session lewat hook `onPasswordChanged`.
- Endpoint baru `DELETE /users/me/identities/{provider}` (Bearer):
  - Guard atomik dalam satu statement: baris hanya terhapus bila user masih
    punya `password_hash <> ''` ATAU masih punya identity lain.
  - 0 baris -> cek keberadaan identity: memang tidak ada -> 404
    `identity not linked`; ada tapi guard menolak -> 409 `cannot unlink the
    last sign-in method`.
  - Magic link tidak dihitung dalam guard: kredensial utama yang dijaga
    tetap password dan identity.
- Link eksplisit (endpoint menautkan provider lain saat sudah login) sengaja
  ditunda: hanya satu provider sekarang; abstraksi provider menunggu
  provider ke-2. Linking tetap lewat login pertama.

## Call graph

```text
POST /auth/supabase  (sudah jalan, tidak diubah)
  -> auth.Supabase.Exchange
    -> Supabase GET /auth/v1/user (token valid + email terkonfirmasi)
    -> user.Service.SignInSupabase
      -> Store.FindByIdentity(provider, id)   -> hit: kembalikan user
      -> Store.FindByEmail(email)             -> hit: LinkIdentity, user lama
      -> Store.Create(Identity)               -> miss: user baru tanpa password
    -> issuePair (access + refresh)

PUT /users/password  (semantik diperluas)
  -> user.Handler.changePassword (principal dari Bearer)
    -> user.Service.UpdatePassword
      -> Store.FindByID
      -> password_hash == "" && current kosong -> Store.UpdatePasswordHash
      -> else bcrypt.CompareHashAndPassword    -> salah: ErrInvalidCredentials
      -> Store.UpdatePasswordHash
    -> onPasswordChanged -> RefreshStore.RevokeAllForUser

DELETE /users/me/identities/{provider}  (baru)
  -> user.Handler.unlink (protect + principal)
    -> user.Service.UnlinkIdentity
      -> Store.UnlinkIdentity (guard atomik: password ada ATAU identity lain)
        -> 0 baris, identity tak ada        -> ErrNotFound      -> 404
        -> 0 baris, guard menolak           -> ErrLastCredential -> 409
```

Tests: call graph sama, hanya R yang ditukar (MemoryStore, fake Supabase
server, app in-memory) - pola test yang sudah ada.

## Files to modify

- `internal/user/user.go` - error baru `ErrLastCredential`.
- `internal/user/service.go` - cabang set-password pertama di
  `UpdatePassword`; use case `UnlinkIdentity`; port `Store` +method
  `UnlinkIdentity` (port hidup di service.go, bukan store.go).
- `internal/user/store_postgres.go` - guarded DELETE atomik + fallback
  keberadaan untuk membedakan 404 vs 409.
- `internal/user/store_memory.go` - mirror semantik yang sama.
- `internal/user/http.go` - route `DELETE /users/me/identities/{provider}`
  (protect), mapping `ErrLastCredential` -> 409.
- `internal/user/{password,identity}_test.go`, `internal/app/app_test.go` -
  test set-password dan matrix unlink.
- `api/openapi.yaml` - deskripsi `/users/password` (current_password
  opsional saat belum ada password) + path baru DELETE.
- `README.md` - tabel endpoint + bullet Auth.

## Reuse

- Port `user.Store` tetap; tambah satu method, bukan store baru.
- Hook `onPasswordChanged` tetap dipakai untuk revoke session.
- `protect` middleware + `principal` resolver tetap dipakai (pola `/users/me`).
- Mapping `mapConstraintError` tidak berubah; unlink tidak lewat insert.
- Pesan error tetap lowercase, satu kalimat, gaya endpoint lain.

## Steps

- [ ] Tulis `learning/account-linking.md` (teori dulu) + kasus di atasnya.
- [ ] `user.go`: tambah `ErrLastCredential`.
- [ ] `store.go`: tambah `UnlinkIdentity` ke port.
- [ ] `store_postgres.go` + `store_memory.go`: implement dengan guard.
- [ ] `service.go`: `UpdatePassword` first-set branch + `UnlinkIdentity`.
- [ ] `http.go`: route DELETE + mapping error baru.
- [ ] Test: first-set password (lalu login password jalan), current terisi
      saat belum ada password -> 401, empty current saat sudah ada password
      -> 401, matrix unlink (204 / 404 / 409), unlink lalu OAuth login lagi
      menautkan user yang sama, oauth-first set password lalu unlink sukses.
- [ ] `api/openapi.yaml` + `README.md`.
- [ ] Gates: `gofmt -l`, `go vet ./...`, `go test -count=1 ./...`,
      `go test -race ./internal/{user,auth,app}`, memserver E2E matrix.

## Verification

- `gofmt -l` kosong; `go vet ./...` bersih; `go test ./...` lulus.
- `-race` pada `internal/user`, `internal/auth`, `internal/app`.
- Matrix memserver `:8790`:
  - OAuth login (fake Supabase) 200 -> `/users/me` `providers: ["swegrowth"]`.
  - `PUT /users/password` tanpa `current_password` 204 -> `POST /auth/login`
    dengan password 200.
  - `PUT /users/password` dengan `current_password` salah 401; kosong saat
    password sudah ada 401.
  - `DELETE /users/me/identities/swegrowth` 204 (password ada) ->
    login OAuth lagi -> user yang sama tertaut kembali.
  - User oauth-first tanpa password mencoba DELETE identity terakhir -> 409.
  - DELETE provider yang tidak tertaut -> 404.
  - `provider_user_id` tetap tidak bocor di semua response.
- YAML openapi valid.

## Open questions

- Unlink guard: password ada ATAU identity lain (magic link tidak dihitung).
  Default: ya, kunci. Alternatif yang ditolak: hitung magic link sebagai
  kredensial -> guard jadi tidak pernah menolak, akun bisa terasa "hilang
  cara masuk" bagi user.
- Set password lewat `PUT /users/password` yang sama, bukan endpoint baru.
  Default: ya, kunci. Frontend membedakan via `/users/me` (password_hash
  tidak diekspos; FE bisa cek gagal login atau tambahkan field `has_password`
  nanti bila perlu).

## Sisa frontend (di luar plan ini)

- `sweg-ai-catalog`: halaman profil perlu UI "set password" untuk user
  oauth-first dan tombol unlink provider; baca `providers` dari `/users/me`.
